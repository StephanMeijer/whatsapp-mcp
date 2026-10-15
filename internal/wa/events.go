// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// saveTimeout bounds each archive write made from the event loop.
const saveTimeout = 30 * time.Second

func (c *Client) handleEvent(evt any) {
	switch e := evt.(type) {
	case *events.Message:
		c.onMessage(e)
	case *events.HistorySync:
		c.onHistorySync(e)
		go c.syncChatNamesWithTimeout()
	case *events.Connected:
		c.log.Info("connected to WhatsApp")
		go c.syncChatNamesWithTimeout()
	case *events.Disconnected:
		c.log.Warn("disconnected from WhatsApp")
	case *events.LoggedOut:
		c.log.Error("logged out by WhatsApp; delete the device and pair again", "reason", e.Reason.String())
	}
}

// syncChatNamesWithTimeout runs syncChatNames off the event loop, which must
// not block on a full pass over the address book.
func (c *Client) syncChatNamesWithTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
	defer cancel()
	c.syncChatNames(ctx)
}

func (c *Client) onMessage(e *events.Message) {
	m, ok := toMessage(e)
	if !ok {
		return
	}
	var chats []store.Chat
	// In a direct chat with someone else, their push name is the best name we have.
	if !e.Info.IsGroup && !e.Info.IsFromMe && e.Info.PushName != "" {
		chats = append(chats, store.Chat{JID: m.ChatJID, Name: e.Info.PushName})
	}
	ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
	defer cancel()
	if err := c.store.Save(ctx, chats, []store.Message{m}); err != nil {
		c.log.Error("archive message", "err", err, "chat", m.ChatJID, "id", m.MessageID)
	}
}

func (c *Client) onHistorySync(e *events.HistorySync) {
	convs := e.Data.GetConversations()
	saved := 0
	for _, conv := range convs {
		chatJID, err := types.ParseJID(conv.GetID())
		if err != nil {
			c.log.Warn("history sync: bad chat JID", "jid", conv.GetID(), "err", err)
			continue
		}
		chat := store.Chat{JID: chatJID.String(), Name: firstNonEmpty(conv.GetName(), conv.GetDisplayName())}
		var msgs []store.Message
		for _, hm := range conv.GetMessages() {
			evt, err := c.cli.ParseWebMessage(chatJID, hm.GetMessage())
			if err != nil {
				continue
			}
			if m, ok := toMessage(evt); ok {
				msgs = append(msgs, m)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
		err = c.store.Save(ctx, []store.Chat{chat}, msgs)
		cancel()
		if err != nil {
			c.log.Error("history sync: archive conversation", "err", err, "chat", chat.JID)
			continue
		}
		saved += len(msgs)
	}
	c.log.Info("history sync archived", "type", e.Data.GetSyncType().String(),
		"conversations", len(convs), "messages", saved)
}

// toMessage converts a whatsmeow message event into an archive row. It reports
// false for messages with nothing to archive (reactions, protocol messages)
// and for edits, which are not handled yet.
func toMessage(e *events.Message) (store.Message, bool) {
	if e.IsEdit {
		return store.Message{}, false
	}
	text, media := messageContent(e.Message)
	if text == "" && media == "" {
		return store.Message{}, false
	}
	return store.Message{
		ChatJID:   e.Info.Chat.ToNonAD().String(),
		MessageID: e.Info.ID,
		SenderJID: e.Info.Sender.ToNonAD().String(),
		Timestamp: e.Info.Timestamp.Unix(),
		FromMe:    e.Info.IsFromMe,
		Text:      text,
		MediaType: media,
	}, true
}

// messageContent extracts the searchable text and the media kind.
func messageContent(m *waE2E.Message) (text, mediaType string) {
	switch {
	case m.GetConversation() != "":
		return m.GetConversation(), ""
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetText(), ""
	case m.GetImageMessage() != nil:
		return m.GetImageMessage().GetCaption(), "image"
	case m.GetVideoMessage() != nil:
		return m.GetVideoMessage().GetCaption(), "video"
	case m.GetAudioMessage() != nil:
		return "", "audio"
	case m.GetDocumentMessage() != nil:
		d := m.GetDocumentMessage()
		return firstNonEmpty(d.GetCaption(), d.GetTitle(), d.GetFileName()), "document"
	case m.GetStickerMessage() != nil:
		return "", "sticker"
	}
	return "", ""
}
