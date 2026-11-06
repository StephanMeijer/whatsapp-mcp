// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// pollCreation returns the poll a message carries, whichever of the poll
// message versions it was sent as, or nil if it is not a poll.
func pollCreation(m *waE2E.Message) *waE2E.PollCreationMessage {
	for _, p := range []*waE2E.PollCreationMessage{
		m.GetPollCreationMessage(),
		m.GetPollCreationMessageV2(),
		m.GetPollCreationMessageV3(),
		m.GetPollCreationMessageV5(),
		m.GetPollCreationMessageV6(),
	} {
		if p != nil {
			return p
		}
	}
	// V4 wraps the poll in a future-proof message.
	if inner := m.GetPollCreationMessageV4().GetMessage(); inner != nil {
		return pollCreation(inner)
	}
	return nil
}

// pollText is the archived text of a poll: the question, then one line per
// option, so that both are found by search.
func pollText(p *waE2E.PollCreationMessage) string {
	var b strings.Builder
	b.WriteString(p.GetName())
	for _, o := range p.GetOptions() {
		b.WriteString("\n- ")
		b.WriteString(o.GetOptionName())
	}
	return b.String()
}

// selectedHashes encodes a vote's chosen option hashes for the archive.
func selectedHashes(hashes [][]byte) string {
	out := make([]string, len(hashes))
	for i, h := range hashes {
		out[i] = hex.EncodeToString(h)
	}
	return strings.Join(out, ",")
}

// onPollVote records a live vote. Votes are encrypted with the poll's
// secret, which whatsmeow only has for polls it saw being sent or received;
// a vote on an older poll is dropped.
func (c *Client) onPollVote(ctx context.Context, e *events.Message, canon func(types.JID) types.JID) {
	pu := e.Message.GetPollUpdateMessage()
	poll := pu.GetPollCreationMessageKey().GetID()
	vote, err := c.cli.DecryptPollVote(ctx, e)
	if err != nil {
		c.log.Debug("decrypt poll vote", "err", err, "poll", poll)
		return
	}
	at := pu.GetSenderTimestampMS() / 1000
	if at == 0 {
		at = e.Info.Timestamp.Unix()
	}
	c.recordVote(ctx, store.PollVote{
		ChatJID: canon(e.Info.Chat).String(), PollID: poll, VoterJID: canon(e.Info.Sender).String(),
		Selected: selectedHashes(vote.GetSelectedOptions()), VotedAt: at,
	})
}

// historyVotes reads the votes a history sync delivers with a poll, already
// decrypted. chat is the conversation the poll is in.
func (c *Client) historyVotes(chat types.JID, archiveChat string, poll *waWeb.WebMessageInfo, canon func(types.JID) types.JID) []store.PollVote {
	var out []store.PollVote
	for _, pu := range poll.GetPollUpdates() {
		key := pu.GetPollUpdateMessageKey()
		voter := chat
		switch {
		case key.GetFromMe():
			voter = c.device.GetJID()
		case key.GetParticipant() != "":
			p, err := types.ParseJID(key.GetParticipant())
			if err != nil {
				continue
			}
			voter = p
		case chat.Server == types.GroupServer:
			continue // a group vote without its voter: the group is not one
		}
		out = append(out, store.PollVote{
			ChatJID: archiveChat, PollID: poll.GetKey().GetID(), VoterJID: canon(voter).String(),
			Selected: selectedHashes(pu.GetVote().GetSelectedOptions()), VotedAt: pu.GetSenderTimestampMS() / 1000,
		})
	}
	return out
}

func (c *Client) recordVote(ctx context.Context, v store.PollVote) {
	if err := c.store.RecordPollVote(ctx, v); err != nil {
		c.log.Error("record poll vote", "err", err, "chat", v.ChatJID, "poll", v.PollID)
	}
}

// PollOption is one answer to a poll and the people who chose it.
type PollOption struct {
	Name   string
	Voters []string // canonical JIDs
}

// PollResult is a poll's current tally.
type PollResult struct {
	Question string
	Multiple bool // more than one answer may be chosen
	Options  []PollOption
	// UnknownVotes counts choices matching no option, such as one added to
	// the poll later.
	UnknownVotes int
}

// TallyPoll counts the votes on an archived poll. Votes name options by the
// SHA-256 hash of their text, so they are matched against the options kept
// in the poll's media reference.
func TallyPoll(m store.Message, votes []store.PollVote) (PollResult, error) {
	if m.MediaType != store.MediaPoll {
		return PollResult{}, errors.New("this message is not a poll")
	}
	var p waE2E.PollCreationMessage
	if len(m.MediaRef) == 0 || proto.Unmarshal(m.MediaRef, &p) != nil {
		return PollResult{}, errors.New("this poll was archived without its options, so its votes can't be counted")
	}
	res := PollResult{Question: p.GetName(), Multiple: p.GetSelectableOptionsCount() != 1}
	names := make([]string, len(p.GetOptions()))
	for i, o := range p.GetOptions() {
		names[i] = o.GetOptionName()
		res.Options = append(res.Options, PollOption{Name: names[i]})
	}
	byHash := make(map[string]int, len(names))
	for i, h := range whatsmeow.HashPollOptions(names) {
		byHash[hex.EncodeToString(h)] = i
	}
	for _, v := range votes {
		for h := range strings.SplitSeq(v.Selected, ",") {
			if h == "" {
				continue
			}
			if i, ok := byHash[h]; ok {
				res.Options[i].Voters = append(res.Options[i].Voters, v.VoterJID)
			} else {
				res.UnknownVotes++
			}
		}
	}
	return res, nil
}

// maxPollOptions is the most options WhatsApp lets a poll have.
const maxPollOptions = 12

// SendPoll sends a poll and archives it. With multiple set, people may
// choose any number of options; otherwise one.
func (c *Client) SendPoll(ctx context.Context, recipient, question string, options []string, multiple bool) (store.Message, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return store.Message{}, errors.New("the poll's question is required")
	}
	if len(options) < 2 || len(options) > maxPollOptions {
		return store.Message{}, fmt.Errorf("a poll needs 2 to %d options, not %d", maxPollOptions, len(options))
	}
	opts := make([]string, len(options))
	seen := make(map[string]bool, len(options))
	for i, o := range options {
		o = strings.TrimSpace(o)
		// Votes name options by the hash of their text, so duplicates can't be told apart.
		if o == "" || seen[o] {
			return store.Message{}, fmt.Errorf("option %d is empty or repeats another", i+1)
		}
		seen[o], opts[i] = true, o
	}
	to, err := c.recipient(recipient)
	if err != nil {
		return store.Message{}, err
	}
	selectable := 1
	if multiple {
		selectable = 0 // WhatsApp's "any number"
	}
	return c.sendAndArchive(ctx, to, c.cli.BuildPollCreation(question, opts, selectable), "poll", "options", len(opts))
}
