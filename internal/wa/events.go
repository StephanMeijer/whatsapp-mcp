// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

const (
	// saveTimeout bounds each archive write made from the event loop.
	saveTimeout = 30 * time.Second
	// reconcileTimeout bounds a full reconcile pass over the archive.
	reconcileTimeout = 2 * time.Minute
	// historySyncTimeout bounds archiving one history sync blob.
	historySyncTimeout = 5 * time.Minute
)

func (c *Client) handleEvent(evt any) {
	switch e := evt.(type) {
	case *events.Message:
		c.onMessage(e)
	case *events.HistorySync:
		c.onHistorySync(e)
		go c.reconcileWithTimeout(false)
	case *events.Connected:
		c.log.Info("connected to WhatsApp")
		go c.reconcileWithTimeout(true)
	case *events.GroupInfo:
		c.onGroupInfo(e)
	case *events.JoinedGroup:
		c.onJoinedGroup(e)
	case *events.CallOffer, *events.CallOfferNotice, *events.CallAccept, *events.CallReject, *events.CallTerminate:
		c.onCallEvent(e)
	case *events.Disconnected:
		c.log.Warn("disconnected from WhatsApp")
	case *events.LoggedOut:
		c.log.Error("logged out by WhatsApp; delete the device and pair again", "reason", e.Reason.String())
	}
}

func (c *Client) onMessage(e *events.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
	defer cancel()
	canon := func(jid types.JID) types.JID { return c.Canonical(ctx, jid) }
	if ch, ok := c.changeFor(ctx, e, canon); ok {
		c.applyChange(ctx, ch)
		return
	}
	m, ok := toMessage(e, canon)
	if !ok {
		return
	}
	var chats []store.Chat
	// In a direct chat with someone else, their push name is the best name we have.
	if !e.Info.IsGroup && !e.Info.IsFromMe && e.Info.PushName != "" {
		chats = append(chats, store.Chat{JID: m.ChatJID, Name: e.Info.PushName})
	}
	if err := c.store.Save(ctx, chats, []store.Message{m}); err != nil {
		c.log.Error("archive message", "err", err, "chat", m.ChatJID, "id", m.MessageID)
		return
	}
	// After saving, so a brand-new group's chat exists to be named.
	if e.Info.IsGroup {
		c.nameGroupIfNeeded(e.Info.Chat)
	}
}

func (c *Client) onHistorySync(e *events.HistorySync) {
	syncCtx, cancelSync := context.WithTimeout(context.Background(), historySyncTimeout)
	defer cancelSync()
	canon := c.memoCanonical(syncCtx)

	// Answers to FetchOlder; resolved after archiving so the waiter sees the messages.
	onDemand := e.Data.GetSyncType() == waHistorySync.HistorySync_ON_DEMAND
	convs := e.Data.GetConversations()
	saved := 0
	for _, conv := range convs {
		chatJID, err := types.ParseJID(conv.GetID())
		if err != nil {
			c.log.Warn("history sync: bad chat JID", "jid", conv.GetID(), "err", err)
			continue
		}
		archiveJID := canon(chatJID)
		// The conversation may carry the phone number itself even when the
		// mapping store doesn't know it yet.
		if archiveJID.Server == types.HiddenUserServer {
			if pn, err := types.ParseJID(conv.GetPnJID()); err == nil && pn.Server == types.DefaultUserServer {
				archiveJID = pn.ToNonAD()
			}
		}
		chat := store.Chat{JID: archiveJID.String(), Name: firstNonEmpty(conv.GetName(), conv.GetDisplayName())}
		var msgs []store.Message
		var changes []change
		for _, hm := range conv.GetMessages() {
			evt, err := c.cli.ParseWebMessage(chatJID, hm.GetMessage())
			if err != nil {
				continue
			}
			if ch, ok := c.changeFor(syncCtx, evt, canon); ok {
				ch.chat = chat.JID
				changes = append(changes, ch)
				continue
			}
			if m, ok := toMessage(evt, canon); ok {
				m.ChatJID = chat.JID
				msgs = append(msgs, m)
			}
		}
		ctx, cancel := context.WithTimeout(syncCtx, saveTimeout)
		err = c.store.Save(ctx, []store.Chat{chat}, msgs)
		if err == nil {
			// After saving, so changes can target originals from the same batch.
			for _, ch := range changes {
				c.applyChange(ctx, ch)
			}
		}
		cancel()
		if err != nil {
			c.log.Error("history sync: archive conversation", "err", err, "chat", chat.JID)
			continue
		}
		saved += len(msgs)
		if onDemand {
			c.fetches.resolve(chat.JID)
		}
	}
	if onDemand {
		c.fetches.resolveSession(e.Notification.GetPeerDataRequestSessionID())
	}
	c.log.Info("history sync archived", "type", e.Data.GetSyncType().String(),
		"conversations", len(convs), "messages", saved)
}

// toMessage converts a whatsmeow message event into an archive row, storing
// chat and sender under the JIDs canon returns. It reports false for messages
// with nothing to archive (reactions, protocol messages) and for edits, which
// toChange handles.
func toMessage(e *events.Message, canon func(types.JID) types.JID) (store.Message, bool) {
	if e.IsEdit {
		return store.Message{}, false
	}
	text, media := messageContent(e.Message)
	if text == "" && media == "" {
		return store.Message{}, false
	}
	return store.Message{
		ChatJID:   canon(e.Info.Chat).String(),
		MessageID: e.Info.ID,
		SenderJID: canon(e.Info.Sender).String(),
		Timestamp: e.Info.Timestamp.Unix(),
		FromMe:    e.Info.IsFromMe,
		Text:      text,
		MediaType: media,
		MediaRef:  mediaRef(e.Message),
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
