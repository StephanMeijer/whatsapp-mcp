// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	// onlineLease is how long the account stays online after a presence
	// call. Online, the phone may stop notifying and contacts see "online",
	// so it is kept short.
	onlineLease = 30 * time.Second
	// presenceWait is how long Presence waits for WhatsApp to report a
	// contact's presence. Nothing comes at all for someone who hides it.
	presenceWait = 5 * time.Second
)

// Presence is what a contact shows of being online.
type Presence struct {
	JID       string `json:"jid"`
	State     string `json:"state" jsonschema:"online, offline, or not_visible when they hide it from you (or WhatsApp did not answer in time)"`
	LastSeen  string `json:"last_seen,omitempty" jsonschema:"RFC 3339; absent when they hide it"`
	ChatState string `json:"chat_state,omitempty" jsonschema:"typing or recording, if they were doing so in your chat during the lookup"`
}

// presenceState is the online lease and the lookups waiting for events.
type presenceState struct {
	mu      sync.Mutex
	lease   int // counts goOnline calls, so an outdated offline timer does nothing
	waiters []*presenceWaiter
}

type presenceWaiter struct {
	jid types.JID
	ch  chan any // *events.Presence and *events.ChatPresence
}

// goOnline marks the account online and schedules going offline again
// when the lease runs out; a call during the lease extends it.
func (c *Client) goOnline(ctx context.Context) error {
	p := &c.presence
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := c.cli.SendPresence(ctx, types.PresenceAvailable); err != nil {
		return fmt.Errorf("go online: %w", err)
	}
	p.lease++
	lease := p.lease
	time.AfterFunc(onlineLease, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.lease != lease || !c.cli.IsLoggedIn() {
			return
		}
		ctx, cancel := context.WithTimeout(c.bgCtx, saveTimeout)
		defer cancel()
		if err := c.cli.SendPresence(ctx, types.PresenceUnavailable); err != nil {
			c.log.Warn("go offline after presence lease", "err", err)
		}
	})
	return nil
}

// Presence reports whether a person is online, when they were last seen,
// and whether they are typing in the direct chat. WhatsApp only tells an
// online account, so this marks the account online for a short while.
func (c *Client) Presence(ctx context.Context, jid types.JID) (Presence, error) {
	if !c.cli.IsLoggedIn() {
		return Presence{}, errNotLoggedIn
	}
	jid = c.Canonical(ctx, jid)
	w := &presenceWaiter{jid: jid, ch: make(chan any, 8)}
	p := &c.presence
	p.mu.Lock()
	p.waiters = append(p.waiters, w)
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.waiters = slices.DeleteFunc(p.waiters, func(o *presenceWaiter) bool { return o == w })
		p.mu.Unlock()
	}()

	if err := c.goOnline(ctx); err != nil {
		return Presence{}, err
	}
	if err := c.cli.SubscribePresence(ctx, jid); err != nil {
		return Presence{}, fmt.Errorf("subscribe to presence: %w", err)
	}
	out := Presence{JID: jid.String(), State: "not_visible"}
	timeout := time.After(presenceWait)
	for {
		select {
		case evt := <-w.ch:
			switch e := evt.(type) {
			case *events.ChatPresence:
				out.ChatState = chatState(e)
			case *events.Presence:
				out.State = "online"
				if e.Unavailable {
					out.State = "offline"
				}
				if !e.LastSeen.IsZero() {
					out.LastSeen = e.LastSeen.UTC().Format(time.RFC3339)
				}
				return out, nil
			}
		case <-timeout:
			return out, nil
		case <-ctx.Done():
			return Presence{}, ctx.Err()
		}
	}
}

func chatState(e *events.ChatPresence) string {
	switch {
	case e.State != types.ChatPresenceComposing:
		return ""
	case e.Media == types.ChatPresenceMediaAudio:
		return "recording"
	}
	return "typing"
}

// SetTyping shows "typing…" or "recording audio…" in a chat, or stops
// showing it. The other side only sees it from an online account, so this
// marks the account online for a short while.
func (c *Client) SetTyping(ctx context.Context, chat types.JID, state types.ChatPresence, media types.ChatPresenceMedia) error {
	if !c.cli.IsLoggedIn() {
		return errNotLoggedIn
	}
	if err := c.goOnline(ctx); err != nil {
		return err
	}
	return c.cli.SendChatPresence(ctx, chat, state, media)
}

// onPresence hands a presence or typing event from (the person, or the
// direct chat) to the lookups waiting for it. Presence is never stored.
func (c *Client) onPresence(from types.JID, evt any) {
	p := &c.presence
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.waiters) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(c.bgCtx, saveTimeout)
	defer cancel()
	// The event may name the person by @lid while the lookup used the number.
	from = c.Canonical(ctx, from)
	for _, w := range p.waiters {
		if w.jid == from {
			select {
			case w.ch <- evt:
			default: // a full buffer means the lookup has plenty to go on
			}
		}
	}
}
