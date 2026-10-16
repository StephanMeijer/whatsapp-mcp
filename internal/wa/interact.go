// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
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

// DeleteMessage deletes one of the account's own messages for everyone and
// marks it deleted in the archive.
func (c *Client) DeleteMessage(ctx context.Context, chat types.JID, target store.Message) error {
	if !c.cli.IsLoggedIn() {
		return errors.New("not logged in to WhatsApp")
	}
	if !target.FromMe {
		return fmt.Errorf("message %s was not sent by you", target.MessageID)
	}
	// An empty sender makes it a revoke of the account's own message.
	if _, err := c.cli.SendMessage(ctx, chat, c.cli.BuildRevoke(chat, types.EmptyJID, target.MessageID)); err != nil {
		return fmt.Errorf("delete message: %w", err)
	}
	if _, err := c.store.MarkDeleted(ctx, chat.String(), target.MessageID); err != nil {
		// The deletion went out; failing to archive it shouldn't fail the call.
		c.log.Error("archive deleted message", "err", err, "chat", chat.String(), "id", target.MessageID)
	}
	c.log.Info("deleted message", "chat", chat.String(), "id", target.MessageID)
	return nil
}

// EditMessage replaces the text, or the caption, of one of the account's own
// messages and applies the edit to the archive. It returns the message as
// edited.
func (c *Client) EditMessage(ctx context.Context, chat types.JID, target store.Message, text string) (store.Message, error) {
	if !c.cli.IsLoggedIn() {
		return store.Message{}, errors.New("not logged in to WhatsApp")
	}
	now := time.Now()
	if err := editable(target, text, now); err != nil {
		return store.Message{}, err
	}
	content, err := editContent(target, text)
	if err != nil {
		return store.Message{}, err
	}
	if _, err := c.cli.SendMessage(ctx, chat, c.cli.BuildEdit(chat, target.MessageID, content)); err != nil {
		return store.Message{}, fmt.Errorf("edit message: %w", err)
	}
	edited := target
	edited.Text, edited.MediaType = messageContent(content)
	edited.EditedAt = now.Unix()
	if _, err := c.store.ApplyEdit(ctx, chat.String(), target.MessageID, edited.Text, edited.MediaType, mediaRef(content, true), edited.EditedAt); err != nil {
		// The edit went out; failing to archive it shouldn't fail the call.
		c.log.Error("archive edited message", "err", err, "chat", chat.String(), "id", target.MessageID)
	}
	c.log.Info("edited message", "chat", chat.String(), "id", target.MessageID)
	return edited, nil
}

// editable reports why m can't be edited to text at now, if it can't.
// WhatsApp only takes edits of your own messages within whatsmeow.EditWindow
// of sending; later edits are refused here rather than risk WhatsApp
// accepting one that recipients never see.
func editable(m store.Message, text string, now time.Time) error {
	switch {
	case !m.FromMe:
		return fmt.Errorf("message %s was not sent by you", m.MessageID)
	case m.Deleted:
		return fmt.Errorf("message %s was deleted", m.MessageID)
	case text == "":
		return errors.New("the new text is empty")
	case now.Sub(time.Unix(m.Timestamp, 0)) > whatsmeow.EditWindow:
		return fmt.Errorf("message %s is too old to edit: WhatsApp only allows edits within %s of sending",
			m.MessageID, whatsmeow.EditWindow)
	}
	switch m.MediaType {
	case "", "image", "video", "document":
	default:
		return fmt.Errorf("message %s is %s without a caption, so it has no text to edit", m.MessageID, articled(m.MediaType))
	}
	if m.MediaType != "" && len(m.MediaRef) == 0 {
		return fmt.Errorf("message %s was archived without its media details, so its caption can't be edited", m.MessageID)
	}
	return nil
}

func articled(kind string) string {
	if kind == "audio" {
		return "audio"
	}
	return "a " + kind
}

// editContent builds the new content for an edit of m: the text itself for
// a text message, or the original media with the new caption. A reply or
// link preview keeps its quote and preview, restored from the archived ref.
func editContent(m store.Message, text string) (*waE2E.Message, error) {
	if m.MediaType == "" {
		if len(m.MediaRef) == 0 {
			return &waE2E.Message{Conversation: proto.String(text)}, nil
		}
		ext := &waE2E.ExtendedTextMessage{}
		if err := proto.Unmarshal(m.MediaRef, ext); err != nil {
			return nil, fmt.Errorf("decode archived text message: %w", err)
		}
		ext.Text = proto.String(text)
		return &waE2E.Message{ExtendedTextMessage: ext}, nil
	}
	sub, _, err := decodeMediaRef(m.MediaType, m.MediaRef)
	if err != nil {
		return nil, err
	}
	switch s := sub.(type) {
	case *waE2E.ImageMessage:
		s.Caption = proto.String(text)
		return &waE2E.Message{ImageMessage: s}, nil
	case *waE2E.VideoMessage:
		s.Caption = proto.String(text)
		return &waE2E.Message{VideoMessage: s}, nil
	case *waE2E.DocumentMessage:
		s.Caption = proto.String(text)
		return &waE2E.Message{DocumentMessage: s}, nil
	}
	return nil, fmt.Errorf("%s messages have no caption to edit", m.MediaType)
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
