// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// markReadLookback is how many of a chat's latest incoming messages get read
// receipts when the chat is marked as read.
const markReadLookback = 20

// React sets the account's reaction to an archived message; an empty emoji
// removes it.
func (c *Client) React(ctx context.Context, chat types.JID, target store.Message, emoji string) error {
	if !c.cli.IsLoggedIn() {
		return errors.New("not logged in to WhatsApp")
	}
	sender := c.wireSender(ctx, chat, target)
	if _, err := c.cli.SendMessage(ctx, chat, c.cli.BuildReaction(chat, sender, target.MessageID, emoji)); err != nil {
		return fmt.Errorf("react: %w", err)
	}
	c.log.Info("reacted to message", "chat", chat.String(), "id", target.MessageID, "removed", emoji == "")
	return nil
}

// MarkChatRead sends read receipts for the chat's latest incoming messages
// (blue ticks for their senders) and marks the chat as read across the
// account's devices, clearing its unread badge.
func (c *Client) MarkChatRead(ctx context.Context, chat types.JID) error {
	if !c.cli.IsLoggedIn() {
		return errors.New("not logged in to WhatsApp")
	}
	incoming, err := c.store.LatestIncoming(ctx, chat.String(), markReadLookback)
	if err != nil {
		return err
	}

	// Receipts go per sender: in groups each participant gets their own.
	type batch struct {
		sender types.JID
		ids    []types.MessageID
		latest time.Time
	}
	var order []string
	bySender := map[string]*batch{}
	for _, m := range incoming {
		b := bySender[m.SenderJID]
		if b == nil {
			b = &batch{sender: c.wireSender(ctx, chat, m), latest: time.Unix(m.Timestamp, 0)}
			bySender[m.SenderJID] = b
			order = append(order, m.SenderJID)
		}
		b.ids = append(b.ids, m.MessageID)
	}
	for _, s := range order {
		b := bySender[s]
		if err := c.cli.MarkRead(ctx, b.ids, b.latest, chat, b.sender); err != nil {
			return fmt.Errorf("send read receipts: %w", err)
		}
	}

	var lastTime time.Time
	var lastKey *waCommon.MessageKey
	if len(incoming) > 0 {
		last := incoming[0]
		lastTime = time.Unix(last.Timestamp, 0)
		lastKey = c.cli.BuildMessageKey(chat, c.wireSender(ctx, chat, last), last.MessageID)
	}
	if err := c.cli.SendAppState(ctx, appstate.BuildMarkChatAsRead(chat, true, lastTime, lastKey)); err != nil {
		return fmt.Errorf("mark chat as read: %w", err)
	}
	c.log.Info("marked chat as read", "chat", chat.String(), "receipts", len(incoming))
	return nil
}

// wireSender returns the sender JID WhatsApp expects in a message key for an
// archived message. The archive keeps senders under their phone number, but
// groups using LID addressing identify participants by LID.
func (c *Client) wireSender(ctx context.Context, chat types.JID, m store.Message) types.JID {
	var own types.JID
	if id := c.cli.Store.ID; id != nil {
		own = id.ToNonAD()
	}
	sender, _ := types.ParseJID(m.SenderJID)
	var senderLID types.JID
	lidGroup := false
	if !m.FromMe && chat.Server == types.GroupServer {
		lidGroup = c.groupModes.isLID(ctx, c, chat)
		if lidGroup && sender.Server == types.DefaultUserServer {
			if lid, err := c.cli.Store.LIDs.GetLIDForPN(ctx, sender); err == nil {
				senderLID = lid
			}
		}
	}
	return chooseWireSender(chat, sender, own, m.FromMe, lidGroup, senderLID)
}

// chooseWireSender is the decision behind wireSender.
func chooseWireSender(chat, sender, own types.JID, fromMe, lidGroup bool, senderLID types.JID) types.JID {
	if fromMe && !own.IsEmpty() {
		return own
	}
	if chat.Server == types.GroupServer && lidGroup && !senderLID.IsEmpty() {
		return senderLID.ToNonAD()
	}
	return sender.ToNonAD()
}

// replyMessage builds a text message quoting an archived message.
func replyMessage(text string, quoted store.Message, quotedSender types.JID) *waE2E.Message {
	quotedText := quoted.Text
	if quotedText == "" && quoted.MediaType != "" {
		quotedText = "[" + quoted.MediaType + "]"
	}
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String(text),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String(quoted.MessageID),
			Participant:   proto.String(quotedSender.ToNonAD().String()),
			QuotedMessage: &waE2E.Message{Conversation: proto.String(quotedText)},
		},
	}}
}

// groupModes caches each group's addressing mode for this run.
type groupModes struct {
	mu   sync.Mutex
	lids map[types.JID]bool
}

func (g *groupModes) isLID(ctx context.Context, c *Client, group types.JID) bool {
	g.mu.Lock()
	lid, known := g.lids[group]
	g.mu.Unlock()
	if known {
		return lid
	}
	info, err := c.cli.GetGroupInfo(ctx, group)
	if err != nil {
		return false // not cached: try again next time
	}
	lid = info.AddressingMode == types.AddressingModeLID
	g.mu.Lock()
	if g.lids == nil {
		g.lids = map[types.JID]bool{}
	}
	g.lids[group] = lid
	g.mu.Unlock()
	return lid
}
