// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// Outcomes of FetchOlder.
const (
	// FetchFetched: the phone answered and older messages were archived.
	FetchFetched = "fetched"
	// FetchNoOlder: the phone answered with nothing older than the archive has.
	FetchNoOlder = "no_older_messages"
	// FetchPending: no answer in time. A late answer is still archived.
	FetchPending = "pending"
)

// fetchInterval is the minimum time between two history requests to the
// phone, across all chats.
const fetchInterval = 2 * time.Second

// FetchResult reports the outcome of FetchOlder.
type FetchResult struct {
	Status  string
	Fetched int64
	// Oldest is the timestamp of the oldest archived message in the chat afterwards.
	Oldest time.Time
}

// FetchOlder asks the primary phone for up to count messages older than the
// oldest one archived for chatJID, and waits up to wait for the answer. The
// answer arrives as an on-demand history sync, which is archived like any
// other; this only reports how many older messages it brought in.
func (c *Client) FetchOlder(ctx context.Context, chatJID string, count int, wait time.Duration) (FetchResult, error) {
	if !c.cli.IsLoggedIn() {
		return FetchResult{}, errors.New("not logged in to WhatsApp")
	}
	jid, err := types.ParseJID(chatJID)
	if err != nil {
		return FetchResult{}, fmt.Errorf("invalid chat JID %q: %w", chatJID, err)
	}
	chat := c.Canonical(ctx, jid)
	oldest, err := c.store.OldestMessage(ctx, chat.String())
	if errors.Is(err, store.ErrNotFound) {
		return FetchResult{}, fmt.Errorf("no archived messages in %s to page back from", chat)
	}
	if err != nil {
		return FetchResult{}, err
	}

	done, release, err := c.fetches.register(chat.String())
	if err != nil {
		return FetchResult{}, err
	}
	defer release()
	if err := c.fetchLimit.allow(time.Now()); err != nil {
		return FetchResult{}, err
	}

	anchor := &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, IsFromMe: oldest.FromMe},
		ID:            oldest.MessageID,
		Timestamp:     time.Unix(oldest.Timestamp, 0),
	}
	resp, err := c.cli.SendPeerMessage(ctx, c.cli.BuildHistorySyncRequest(anchor, count))
	if err != nil {
		return FetchResult{}, fmt.Errorf("ask phone for older messages: %w", err)
	}
	c.fetches.setSession(chat.String(), resp.ID)
	c.log.Info("requested older messages from phone", "chat", chat.String(), "count", count, "request", resp.ID)

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		return FetchResult{Status: FetchPending, Oldest: time.Unix(oldest.Timestamp, 0)}, nil
	case <-ctx.Done():
		return FetchResult{}, ctx.Err()
	}

	n, err := c.store.CountBefore(ctx, chat.String(), oldest.Timestamp)
	if err != nil {
		return FetchResult{}, err
	}
	res := FetchResult{Status: FetchNoOlder, Fetched: n, Oldest: time.Unix(oldest.Timestamp, 0)}
	if n > 0 {
		res.Status = FetchFetched
		if first, err := c.store.OldestMessage(ctx, chat.String()); err == nil {
			res.Oldest = time.Unix(first.Timestamp, 0)
		}
	}
	return res, nil
}

// pendingFetches tracks history requests waiting for the phone's answer,
// one per chat. Answers are matched by chat (the conversations they carry)
// and by request ID (the notification's session ID), since an answer with
// nothing older may carry no conversation at all. Each request only ever
// removes its own entries, so a finished request cannot disturb a newer one
// for the same chat.
type pendingFetches struct {
	mu        sync.Mutex
	byChat    map[string]*pendingFetch
	bySession map[string]*pendingFetch
}

type pendingFetch struct {
	chat    string
	session string
	done    chan struct{}
}

// register starts waiting for chat. The returned release must be called when
// done waiting. Only one request per chat may be pending.
func (p *pendingFetches) register(chat string) (<-chan struct{}, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.byChat == nil {
		p.byChat = map[string]*pendingFetch{}
		p.bySession = map[string]*pendingFetch{}
	}
	if _, busy := p.byChat[chat]; busy {
		return nil, nil, fmt.Errorf("a request for older messages in %s is already pending", chat)
	}
	f := &pendingFetch{chat: chat, done: make(chan struct{})}
	p.byChat[chat] = f
	release := func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.forget(f)
	}
	return f.done, release, nil
}

// setSession records the request ID sent for chat's pending request.
func (p *pendingFetches) setSession(chat, requestID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if f := p.byChat[chat]; f != nil && requestID != "" {
		f.session = requestID
		p.bySession[requestID] = f
	}
}

// resolve wakes the request waiting for chat, if any.
func (p *pendingFetches) resolve(chat string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if f := p.byChat[chat]; f != nil {
		close(f.done)
		p.forget(f)
	}
}

// resolveSession wakes the request with this ID, if it is still waiting.
func (p *pendingFetches) resolveSession(requestID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if f := p.bySession[requestID]; f != nil {
		if p.byChat[f.chat] == f {
			close(f.done)
		}
		p.forget(f)
	}
}

// forget removes f's entries, leaving any newer request's alone. Callers hold p.mu.
func (p *pendingFetches) forget(f *pendingFetch) {
	if p.byChat[f.chat] == f {
		delete(p.byChat, f.chat)
	}
	if f.session != "" && p.bySession[f.session] == f {
		delete(p.bySession, f.session)
	}
}

// rateLimit allows one event per interval.
type rateLimit struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
}

func (r *rateLimit) allow(now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if wait := r.interval - now.Sub(r.last); !r.last.IsZero() && wait > 0 {
		return fmt.Errorf("too many history requests; try again in %s", wait.Round(100*time.Millisecond))
	}
	r.last = now
	return nil
}
