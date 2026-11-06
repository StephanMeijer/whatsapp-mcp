// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

type tools struct {
	wa        WhatsApp
	store     *store.Store
	policy    *policy
	mediaRoot *os.Root // nil unless -media-dir is set
}

func registerTools(s *mcp.Server, t *tools) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "connection_status",
		Description: "Report whether the server is connected and paired to a WhatsApp account.",
		Annotations: readOnly,
	}, t.connectionStatus)
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_chats",
		Description: "List chats, pinned first, then most recently active, optionally filtered by name or JID. " +
			"Archived chats are left out unless archived is include or only.",
		Annotations: readOnly,
	}, t.listChats)
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_messages",
		Description: "List a chat's messages, newest first. Page backwards by passing the id of the last message " +
			"returned as before_id; before (a time) is only for jumping to a date and can skip messages.",
		Annotations: readOnly,
	}, t.listMessages)
	mcp.AddTool(s, &mcp.Tool{
		Name: "search_messages",
		Description: "Full-text search message text and captions, optionally within one chat. All words must match. " +
			"Use message_context with a result's chat_jid and id to see the conversation around it.",
		Annotations: readOnly,
	}, t.searchMessages)
	mcp.AddTool(s, &mcp.Tool{
		Name: "recent_messages",
		Description: "Messages received since a point in time across all chats, for questions like \"what did I miss?\". " +
			"Grouped per chat, most recently active chat first, messages oldest first within a chat. " +
			"Excludes your own messages unless include_from_me is set. If truncated is true, only the newest " +
			"messages were returned: narrow since, or use list_messages for a single chat.",
		Annotations: readOnly,
	}, t.recentMessages)
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_status_updates",
		Description: "Status updates (stories) from your contacts, grouped by person, by default from the last " +
			"24 hours. They are not part of list_chats, recent_messages or search_messages. Fetch a photo or video " +
			"with get_media using chat_jid " + store.StatusChat + " and the update's id; WhatsApp may delete status " +
			"media sooner than chat media.",
		Annotations: readOnly,
	}, t.listStatusUpdates)
	mcp.AddTool(s, &mcp.Tool{
		Name: "message_context",
		Description: "The conversation around one message: up to `before` messages before it and `after` after it " +
			"in the same chat, oldest first. messages[target_index] is the requested message. " +
			"Pass the chat_jid and id from a search_messages or list_messages result.",
		Annotations: readOnly,
	}, t.messageContext)
	mcp.AddTool(s, &mcp.Tool{
		Name: "message_receipts",
		Description: "Who has received, read or played one of your messages, with times; in a group, per member. " +
			"People who turned off read receipts only ever show as delivered. Receipts are only known for " +
			"messages sent while this server was running.",
		Annotations: readOnly,
	}, t.messageReceipts)
	mcp.AddTool(s, &mcp.Tool{
		Name: "poll_results",
		Description: "A poll's options with how many people chose each and who. A later vote replaces an " +
			"earlier one. Votes are missing only on polls this server never received.",
		Annotations: readOnly,
	}, t.pollResults)
	mcp.AddTool(s, &mcp.Tool{
		Name: "fetch_older_messages",
		Description: "Ask the phone for messages older than the oldest one archived in a chat, and archive them. " +
			"Call it again to page further back. Needs the phone to be online. status is \"fetched\" (with the " +
			"number of older messages now available via list_messages), \"no_older_messages\" (the archive " +
			"reaches the start of the chat on the phone), or \"pending\" (no answer in time; a late answer is " +
			"still archived, so check list_messages later).",
		// It only reads: the request goes to the account's own phone, never to a contact.
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, t.fetchOlderMessages)
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_media",
		Description: "Download the photo, sticker, video, voice note or document attached to a message " +
			"(a message whose media_type is set, other than poll, location or contact). Images are returned as images you can look at. " +
			"Files up to 16 MB; media WhatsApp no longer stores cannot be retrieved.",
		Annotations: readOnly,
	}, t.getMedia)
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_calls",
		Description: "Incoming calls, newest first: who called, audio or video, when, and whether it is still " +
			"ringing, was answered (on one of your devices), rejected, missed or ended (answered, then hung up). " +
			"Use it to find a ringing call to reject. For calls still marked answered the end time is not known: WhatsApp only tells the device that took the call.",
		Annotations: readOnly,
	}, t.listCalls)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_contacts",
		Description: "Search the address book by name, push name or phone number.",
		Annotations: readOnly,
	}, t.searchContacts)
	mcp.AddTool(s, &mcp.Tool{
		Name: "check_number",
		Description: "Find out whether phone numbers are on WhatsApp, and get the JID to pass to send_message. " +
			"Give each number with its country code, as +31612345678 or 0031612345678; at most 10 per call. " +
			"Only look up numbers you mean to message: WhatsApp treats bulk or speculative lookups as " +
			"enumeration and may ban the account.",
		Annotations: readOnly,
	}, t.checkNumber)
	mcp.AddTool(s, &mcp.Tool{
		Name: "group_info",
		Description: "Show a group's description, settings and members with their names and admin roles. " +
			"chat_jid must be a group JID (ending in @g.us), from list_chats.",
		Annotations: readOnly,
	}, t.groupInfo)
	mcp.AddTool(s, &mcp.Tool{
		Name: "contact_info",
		Description: "A contact's about text, verified business name and whether they have a profile picture. " +
			"jid is a JID or an international phone number. An about text hidden from you looks the same as none.",
		Annotations: readOnly,
	}, t.contactInfo)
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_profile_picture",
		Description: "The profile picture of a contact or group, returned as an image you can look at. " +
			"Set preview for the small thumbnail. Pictures hidden from you, or not set, give an error saying so.",
		Annotations: readOnly,
	}, t.getProfilePicture)

	// Write tools: registered only when not read-only, and each must check
	// its recipient with t.policy.
	if t.policy.readOnly {
		return
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "send_message",
		Description: "Send a text message. This is delivered immediately and cannot be undone. " +
			"Set reply_to to a message id in the same chat to send it as a reply quoting that message.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true)},
	}, t.sendMessage)
	mcp.AddTool(s, &mcp.Tool{
		Name: "send_media",
		Description: "Send a photo, video, audio file or document, delivered immediately. Pass the file as " +
			"base64 in data, or as path to a file in the server's media folder if one is configured. " +
			"Up to 16 MB. Images must be JPEG or PNG, video MP4, audio OGG, MP3, M4A or AAC.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true)},
	}, t.sendMedia)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "send_location",
		Description: "Send a location pin, with an optional place name and address, delivered immediately.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true)},
	}, t.sendLocation)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "send_contact",
		Description: "Send a contact card with a name and phone number, delivered immediately; the recipient can tap it to chat or save the contact.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true)},
	}, t.sendContact)
	mcp.AddTool(s, &mcp.Tool{
		Name: "forward_message",
		Description: "Forward an archived message to another chat, labelled \"Forwarded\" as on the phone. " +
			"Text, images, videos, audio and documents can be forwarded; a file is downloaded and sent again, " +
			"so one that has expired from WhatsApp's servers fails with an error.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true)},
	}, t.forwardMessage)
	mcp.AddTool(s, &mcp.Tool{
		Name: "send_poll",
		Description: "Send a poll with 2 to 12 options, delivered immediately. People choose one option, or any " +
			"number with multiple set. See the answers with poll_results.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true)},
	}, t.sendPoll)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "reject_call",
		Description: "Decline an incoming call that is still ringing; the caller sees it as declined.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true)},
	}, t.rejectCall)
	mcp.AddTool(s, &mcp.Tool{
		Name: "react",
		Description: "React to a message with an emoji, visible to everyone in the chat. " +
			"An empty emoji removes your reaction.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true), IdempotentHint: true},
	}, t.react)
	mcp.AddTool(s, &mcp.Tool{
		Name: "edit_message",
		Description: "Change the text, or the caption, of one of your own messages sent in the last 20 minutes. " +
			"Everyone in the chat sees the new text, marked as edited. A reply keeps its quote, except for " +
			"replies archived by an older version of this server, which lose it.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true), IdempotentHint: true},
	}, t.editMessage)
	mcp.AddTool(s, &mcp.Tool{
		Name: "delete_message",
		Description: "Delete one of your own messages for everyone in the chat. This cannot be undone. " +
			"Only messages you sent can be deleted, and WhatsApp only allows it for a limited time after sending.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true), DestructiveHint: new(true)},
	}, t.deleteMessage)
	mcp.AddTool(s, &mcp.Tool{
		Name: "mark_read",
		Description: "Mark a chat as read: senders of its latest messages see blue ticks, and the unread " +
			"badge clears on all your devices.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true), IdempotentHint: true},
	}, t.markRead)
	mcp.AddTool(s, &mcp.Tool{
		Name: "set_chat_state",
		Description: "Archive or unarchive, pin or unpin, and mute or unmute a chat, synced to your phone. " +
			"Archiving also unpins. mute is off, forever, or a duration such as 8h or 168h.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true), IdempotentHint: true},
	}, t.setChatState)
	mcp.AddTool(s, &mcp.Tool{
		Name: "set_disappearing_timer",
		Description: "Turn disappearing messages on or off for a chat, as on the phone: new messages then vanish " +
			"after the timer. timer is off, 24h, 7d or 90d. In a direct chat WhatsApp announces the change in the chat.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true), IdempotentHint: true},
	}, t.setDisappearingTimer)
	if t.policy.groupAdmin {
		registerGroupAdmin(s, t)
	}
	mcp.AddTool(s, &mcp.Tool{
		Name: "post_status",
		Description: "Post a status update (story): text, or a photo or video with an optional caption. It goes " +
			"to everyone your phone's status privacy settings allow, and disappears after 24 hours.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true)},
	}, t.postStatus)
}

// Message is the tool-facing form of store.Message.
type Message struct {
	ChatJID string `json:"chat_jid"`
	ID      string `json:"id"`
	Sender  string `json:"sender"`
	// SenderName is filled at call time from the contacts store.
	SenderName string `json:"sender_name,omitempty" jsonschema:"display name: the saved contact name, otherwise their WhatsApp push name"`
	Timestamp  string `json:"timestamp" jsonschema:"RFC 3339"`
	FromMe     bool   `json:"from_me"`
	Text       string `json:"text,omitempty"`
	MediaType  string `json:"media_type,omitempty" jsonschema:"image, video, audio, document, sticker, poll, location or contact; empty for text. A poll's text is its question followed by one '- option' line per option; a location's is its name and address, then its coordinates and a map link; a contact's is one 'Name: +number' line per card"`
	EditedAt   string `json:"edited_at,omitempty" jsonschema:"RFC 3339; set if the message was edited"`
	Status     string `json:"status,omitempty" jsonschema:"your own messages only: sent, delivered, read or played, the furthest any recipient got; see message_receipts"`
	Forwarded  bool   `json:"forwarded,omitempty" jsonschema:"true if the message was forwarded from another chat"`
	// ForwardedManyTimes mirrors the phone's "Forwarded many times" label.
	ForwardedManyTimes bool `json:"forwarded_many_times,omitempty" jsonschema:"true if it has been forwarded at least 5 times along the way"`
}

// manyTimesScore is the forwarding score from which the phone labels a
// message "Forwarded many times".
const manyTimesScore = 5

// toMessages converts archive rows to tool output, naming senders with one
// contacts lookup for the whole batch. A failed lookup only loses the names.
func (t *tools) toMessages(ctx context.Context, msgs []store.Message) []Message {
	senders := make([]string, len(msgs))
	for i, m := range msgs {
		senders[i] = m.SenderJID
	}
	names := t.senderNames(ctx, senders)
	out := make([]Message, len(msgs))
	for i, m := range msgs {
		out[i] = Message{
			ChatJID:    m.ChatJID,
			ID:         m.MessageID,
			Sender:     m.SenderJID,
			SenderName: names[m.SenderJID],
			Timestamp:  formatUnix(m.Timestamp),
			FromMe:     m.FromMe,
			Text:       m.Text,
			MediaType:  string(m.MediaType),
			Forwarded:  m.Forwarded,
			// The phone shows the label only on forwarded messages.
			ForwardedManyTimes: m.Forwarded && m.ForwardingScore >= manyTimesScore,
		}
		if m.EditedAt > 0 {
			out[i].EditedAt = formatUnix(m.EditedAt)
		}
		if m.FromMe {
			out[i].Status = m.Status
		}
	}
	return out
}

// senderNames looks up contact names for sender JIDs in one call; a failed
// lookup returns nil, so callers simply go without names.
func (t *tools) senderNames(ctx context.Context, senders []string) map[string]string {
	seen := map[string]bool{}
	var jids []types.JID
	for _, s := range senders {
		if seen[s] {
			continue
		}
		seen[s] = true
		if jid, err := types.ParseJID(s); err == nil {
			jids = append(jids, jid)
		}
	}
	if len(jids) == 0 {
		return nil
	}
	byJID, err := t.wa.Names(ctx, jids)
	if err != nil {
		t.policy.log.Warn("look up sender names; returning messages without them", "err", err)
		return nil
	}
	names := make(map[string]string, len(byJID))
	for jid, name := range byJID {
		names[jid.String()] = name
	}
	return names
}

func formatUnix(sec int64) string { return time.Unix(sec, 0).UTC().Format(time.RFC3339) }

func (t *tools) connectionStatus(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, wa.Status, error) {
	return nil, t.wa.Status(), nil
}

// canonicalChat maps a chat JID given by a client to the archive's form, so
// an @lid reference finds a chat archived under the phone number. Input that
// isn't a JID is passed through; it simply won't match anything. "" stays
// "", meaning no chat filter.
func (t *tools) canonicalChat(ctx context.Context, chatJID string) string {
	jid, err := types.ParseJID(chatJID)
	if err != nil {
		return chatJID
	}
	return t.wa.Canonical(ctx, jid).String()
}
