// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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
		Name:        "list_messages",
		Description: "List a chat's messages, newest first. Page backwards by passing the oldest timestamp seen as 'before'.",
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
			"(a message whose media_type is set). Images are returned as images you can look at. " +
			"Files up to 16 MB; media WhatsApp no longer stores cannot be retrieved.",
		Annotations: readOnly,
	}, t.getMedia)
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_calls",
		Description: "Incoming calls, newest first: who called, audio or video, when, and whether it is still " +
			"ringing, was answered (on one of your devices), rejected or missed. Use it to find a ringing call " +
			"to reject. For answered calls the end time is not known: WhatsApp only tells the device that took the call.",
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
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true)},
	}, t.sendMessage)
	mcp.AddTool(s, &mcp.Tool{
		Name: "send_media",
		Description: "Send a photo, video, audio file or document, delivered immediately. Pass the file as " +
			"base64 in data, or as path to a file in the server's media folder if one is configured. " +
			"Up to 16 MB. Images must be JPEG or PNG, video MP4, audio OGG, MP3, M4A or AAC.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true)},
	}, t.sendMedia)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "reject_call",
		Description: "Decline an incoming call that is still ringing; the caller sees it as declined.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true)},
	}, t.rejectCall)
	mcp.AddTool(s, &mcp.Tool{
		Name: "react",
		Description: "React to a message with an emoji, visible to everyone in the chat. " +
			"An empty emoji removes your reaction.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true), IdempotentHint: true},
	}, t.react)
	mcp.AddTool(s, &mcp.Tool{
		Name: "edit_message",
		Description: "Change the text, or the caption, of one of your own messages sent in the last 20 minutes. " +
			"Everyone in the chat sees the new text, marked as edited. A reply keeps its quote, except for " +
			"replies archived by an older version of this server, which lose it.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true), IdempotentHint: true},
	}, t.editMessage)
	mcp.AddTool(s, &mcp.Tool{
		Name: "delete_message",
		Description: "Delete one of your own messages for everyone in the chat. This cannot be undone. " +
			"Only messages you sent can be deleted, and WhatsApp only allows it for a limited time after sending.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true), DestructiveHint: ptr(true)},
	}, t.deleteMessage)
	mcp.AddTool(s, &mcp.Tool{
		Name: "mark_read",
		Description: "Mark a chat as read: senders of its latest messages see blue ticks, and the unread " +
			"badge clears on all your devices.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true), IdempotentHint: true},
	}, t.markRead)
	mcp.AddTool(s, &mcp.Tool{
		Name: "set_chat_state",
		Description: "Archive or unarchive, pin or unpin, and mute or unmute a chat, synced to your phone. " +
			"Archiving also unpins. mute is off, forever, or a duration such as 8h or 168h.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true), IdempotentHint: true},
	}, t.setChatState)
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
	MediaType  string `json:"media_type,omitempty" jsonschema:"image, video, audio, document or sticker; empty for text"`
	EditedAt   string `json:"edited_at,omitempty" jsonschema:"RFC 3339; set if the message was edited"`
	Status     string `json:"status,omitempty" jsonschema:"your own messages only: sent, delivered, read or played, the furthest any recipient got; see message_receipts"`
}

// toMessages converts archive rows to tool output, naming senders with one
// contacts lookup for the whole batch. A failed lookup only loses the names.
func (t *tools) toMessages(ctx context.Context, msgs []store.Message) []Message {
	names := t.senderNames(ctx, msgs)
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
			MediaType:  m.MediaType,
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

func (t *tools) senderNames(ctx context.Context, msgs []store.Message) map[string]string {
	seen := map[string]bool{}
	var jids []types.JID
	for _, m := range msgs {
		if seen[m.SenderJID] {
			continue
		}
		seen[m.SenderJID] = true
		if jid, err := types.ParseJID(m.SenderJID); err == nil {
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

type ListChatsIn struct {
	Query    string `json:"query,omitempty" jsonschema:"case-insensitive substring of the chat name or JID"`
	Archived string `json:"archived,omitempty" jsonschema:"exclude (default), include or only"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum results (default 50, max 500)"`
}

type Chat struct {
	JID           string `json:"jid"`
	Name          string `json:"name,omitempty"`
	LastMessageAt string `json:"last_message_at,omitempty" jsonschema:"RFC 3339"`
	Archived      bool   `json:"archived,omitempty"`
	Pinned        bool   `json:"pinned,omitempty"`
	MutedUntil    string `json:"muted_until,omitempty" jsonschema:"RFC 3339, or forever"`
}

func toChat(c store.Chat) Chat {
	out := Chat{JID: c.JID, Name: c.Name, Archived: c.Archived, Pinned: c.Pinned}
	if c.LastMessageAt > 0 {
		out.LastMessageAt = formatUnix(c.LastMessageAt)
	}
	switch {
	case c.MutedUntil < 0:
		out.MutedUntil = "forever"
	case c.MutedUntil > 0:
		out.MutedUntil = formatUnix(c.MutedUntil)
	}
	return out
}

type ListChatsOut struct {
	Chats []Chat `json:"chats"`
}

func (t *tools) listChats(ctx context.Context, _ *mcp.CallToolRequest, in ListChatsIn) (*mcp.CallToolResult, ListChatsOut, error) {
	var archived store.ArchiveFilter
	switch in.Archived {
	case "", "exclude":
	case "include":
		archived = store.IncludeArchived
	case "only":
		archived = store.OnlyArchived
	default:
		return nil, ListChatsOut{}, fmt.Errorf("archived: want exclude, include or only, not %q", in.Archived)
	}
	chats, err := t.store.ListChats(ctx, in.Query, archived, in.Limit)
	if err != nil {
		return nil, ListChatsOut{}, err
	}
	out := ListChatsOut{Chats: make([]Chat, len(chats))}
	for i, c := range chats {
		out.Chats[i] = toChat(c)
	}
	return nil, out, nil
}

type ListMessagesIn struct {
	ChatJID string `json:"chat_jid" jsonschema:"the chat's JID, from list_chats"`
	Before  string `json:"before,omitempty" jsonschema:"RFC 3339; only messages sent before this time"`
	Limit   int    `json:"limit,omitempty" jsonschema:"maximum results (default 50, max 500)"`
}

type MessagesOut struct {
	Messages []Message `json:"messages"`
}

func (t *tools) listMessages(ctx context.Context, _ *mcp.CallToolRequest, in ListMessagesIn) (*mcp.CallToolResult, MessagesOut, error) {
	if in.ChatJID == "" {
		return nil, MessagesOut{}, errors.New("chat_jid is required")
	}
	var before time.Time
	if in.Before != "" {
		var err error
		if before, err = time.Parse(time.RFC3339, in.Before); err != nil {
			return nil, MessagesOut{}, fmt.Errorf("before: want RFC 3339, e.g. 2026-01-02T15:04:05Z: %w", err)
		}
	}
	msgs, err := t.store.ListMessages(ctx, t.canonicalChat(ctx, in.ChatJID), before, in.Limit)
	if err != nil {
		return nil, MessagesOut{}, err
	}
	return nil, MessagesOut{Messages: t.toMessages(ctx, msgs)}, nil
}

type SearchMessagesIn struct {
	Query   string `json:"query" jsonschema:"words to search for"`
	ChatJID string `json:"chat_jid,omitempty" jsonschema:"restrict to this chat"`
	Limit   int    `json:"limit,omitempty" jsonschema:"maximum results (default 50, max 500)"`
}

func (t *tools) searchMessages(ctx context.Context, _ *mcp.CallToolRequest, in SearchMessagesIn) (*mcp.CallToolResult, MessagesOut, error) {
	chat := in.ChatJID
	if chat != "" {
		chat = t.canonicalChat(ctx, chat)
	}
	msgs, err := t.store.SearchMessages(ctx, in.Query, chat, in.Limit)
	if err != nil {
		return nil, MessagesOut{}, err
	}
	return nil, MessagesOut{Messages: t.toMessages(ctx, msgs)}, nil
}

// recentDefaultLimit is higher than other tools' default: a catch-up spans many chats.
const recentDefaultLimit = 200

type RecentMessagesIn struct {
	Since         string `json:"since" jsonschema:"RFC 3339; messages at or after this time"`
	Until         string `json:"until,omitempty" jsonschema:"RFC 3339; only messages before this time"`
	ChatJID       string `json:"chat_jid,omitempty" jsonschema:"restrict to this chat"`
	IncludeFromMe bool   `json:"include_from_me,omitempty" jsonschema:"also return messages you sent (default false)"`
	Limit         int    `json:"limit,omitempty" jsonschema:"maximum messages in total (default 200, max 500)"`
}

type ChatMessages struct {
	JID      string    `json:"jid"`
	Name     string    `json:"name,omitempty"`
	Messages []Message `json:"messages" jsonschema:"oldest first"`
}

type RecentMessagesOut struct {
	Chats     []ChatMessages `json:"chats" jsonschema:"most recently active chat first"`
	Total     int            `json:"total" jsonschema:"number of messages returned"`
	Truncated bool           `json:"truncated" jsonschema:"true if older messages in the window were left out"`
}

func (t *tools) recentMessages(ctx context.Context, _ *mcp.CallToolRequest, in RecentMessagesIn) (*mcp.CallToolResult, RecentMessagesOut, error) {
	if in.Since == "" {
		return nil, RecentMessagesOut{}, errors.New("since is required, e.g. 2026-01-02T00:00:00Z")
	}
	since, err := time.Parse(time.RFC3339, in.Since)
	if err != nil {
		return nil, RecentMessagesOut{}, fmt.Errorf("since: want RFC 3339, e.g. 2026-01-02T00:00:00Z: %w", err)
	}
	var until time.Time
	if in.Until != "" {
		if until, err = time.Parse(time.RFC3339, in.Until); err != nil {
			return nil, RecentMessagesOut{}, fmt.Errorf("until: want RFC 3339: %w", err)
		}
		if !until.After(since) {
			return nil, RecentMessagesOut{}, errors.New("until must be after since")
		}
	}
	limit := in.Limit
	if limit <= 0 {
		limit = recentDefaultLimit
	}
	chat := in.ChatJID
	if chat != "" {
		chat = t.canonicalChat(ctx, chat)
	}

	msgs, truncated, err := t.store.RecentMessages(ctx, store.RecentQuery{
		Since: since, Until: until, ChatJID: chat, IncludeFromMe: in.IncludeFromMe, Limit: limit,
	})
	if err != nil {
		return nil, RecentMessagesOut{}, err
	}

	// msgs is newest first, so chats appear in order of latest activity.
	out := RecentMessagesOut{Chats: []ChatMessages{}, Total: len(msgs), Truncated: truncated}
	index := map[string]int{}
	for _, m := range t.toMessages(ctx, msgs) {
		i, ok := index[m.ChatJID]
		if !ok {
			i = len(out.Chats)
			index[m.ChatJID] = i
			out.Chats = append(out.Chats, ChatMessages{JID: m.ChatJID})
		}
		out.Chats[i].Messages = append(out.Chats[i].Messages, m)
	}
	jids := make([]string, len(out.Chats))
	for i := range out.Chats {
		slices.Reverse(out.Chats[i].Messages) // oldest first within a chat
		jids[i] = out.Chats[i].JID
	}
	chats, err := t.store.ChatsByJID(ctx, jids)
	if err != nil {
		return nil, RecentMessagesOut{}, err
	}
	for i := range out.Chats {
		out.Chats[i].Name = chats[out.Chats[i].JID].Name
	}
	return nil, out, nil
}

const (
	contextDefault = 5
	contextMax     = 50
)

type MessageContextIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"the chat's JID, from a search or list result"`
	MessageID string `json:"message_id" jsonschema:"the message id, from a search or list result"`
	Before    *int   `json:"before,omitempty" jsonschema:"messages before it (default 5, max 50)"`
	After     *int   `json:"after,omitempty" jsonschema:"messages after it (default 5, max 50)"`
}

type MessageContextOut struct {
	ChatJID     string    `json:"chat_jid"`
	ChatName    string    `json:"chat_name,omitempty"`
	Messages    []Message `json:"messages" jsonschema:"oldest first"`
	TargetIndex int       `json:"target_index" jsonschema:"index of the requested message in messages"`
}

func contextCount(name string, v *int) (int, error) {
	if v == nil {
		return contextDefault, nil
	}
	if *v < 0 || *v > contextMax {
		return 0, fmt.Errorf("%s must be between 0 and %d", name, contextMax)
	}
	return *v, nil
}

func (t *tools) messageContext(ctx context.Context, _ *mcp.CallToolRequest, in MessageContextIn) (*mcp.CallToolResult, MessageContextOut, error) {
	if in.ChatJID == "" || in.MessageID == "" {
		return nil, MessageContextOut{}, errors.New("chat_jid and message_id are required")
	}
	before, err := contextCount("before", in.Before)
	if err != nil {
		return nil, MessageContextOut{}, err
	}
	after, err := contextCount("after", in.After)
	if err != nil {
		return nil, MessageContextOut{}, err
	}
	chat := t.canonicalChat(ctx, in.ChatJID)

	msgs, target, err := t.store.MessageContext(ctx, chat, in.MessageID, before, after)
	if errors.Is(err, store.ErrNotFound) {
		return nil, MessageContextOut{}, fmt.Errorf("message %s not found in chat %s; only archived messages are available",
			in.MessageID, chat)
	}
	if err != nil {
		return nil, MessageContextOut{}, err
	}
	chats, err := t.store.ChatsByJID(ctx, []string{chat})
	if err != nil {
		return nil, MessageContextOut{}, err
	}
	return nil, MessageContextOut{
		ChatJID:     chat,
		ChatName:    chats[chat].Name,
		Messages:    t.toMessages(ctx, msgs),
		TargetIndex: target,
	}, nil
}

const (
	fetchMaxCount    = 50
	fetchMaxWait     = 60
	fetchDefaultWait = 30
)

type FetchOlderIn struct {
	ChatJID     string `json:"chat_jid" jsonschema:"the chat's JID"`
	Count       int    `json:"count,omitempty" jsonschema:"messages to request (default 50, max 50)"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"how long to wait for the phone (default 30, max 60)"`
}

type FetchOlderOut struct {
	Status          string `json:"status" jsonschema:"fetched, no_older_messages or pending"`
	Fetched         int64  `json:"fetched" jsonschema:"older messages now archived"`
	OldestTimestamp string `json:"oldest_timestamp" jsonschema:"RFC 3339; the oldest archived message in the chat"`
}

func (t *tools) fetchOlderMessages(ctx context.Context, _ *mcp.CallToolRequest, in FetchOlderIn) (*mcp.CallToolResult, FetchOlderOut, error) {
	if in.ChatJID == "" {
		return nil, FetchOlderOut{}, errors.New("chat_jid is required")
	}
	count := in.Count
	if count == 0 {
		count = fetchMaxCount
	}
	if count < 1 || count > fetchMaxCount {
		return nil, FetchOlderOut{}, fmt.Errorf("count must be between 1 and %d", fetchMaxCount)
	}
	wait := in.WaitSeconds
	if wait == 0 {
		wait = fetchDefaultWait
	}
	if wait < 1 || wait > fetchMaxWait {
		return nil, FetchOlderOut{}, fmt.Errorf("wait_seconds must be between 1 and %d", fetchMaxWait)
	}

	res, err := t.wa.FetchOlder(ctx, t.canonicalChat(ctx, in.ChatJID), count, time.Duration(wait)*time.Second)
	if err != nil {
		return nil, FetchOlderOut{}, err
	}
	return nil, FetchOlderOut{
		Status:          res.Status,
		Fetched:         res.Fetched,
		OldestTimestamp: formatUnix(res.Oldest.Unix()),
	}, nil
}

type MessageReceiptsIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"the chat's JID"`
	MessageID string `json:"message_id" jsonschema:"the id of a message you sent"`
}

type ReceiptOut struct {
	JID         string `json:"jid"`
	Name        string `json:"name,omitempty"`
	DeliveredAt string `json:"delivered_at,omitempty" jsonschema:"RFC 3339"`
	ReadAt      string `json:"read_at,omitempty" jsonschema:"RFC 3339"`
	PlayedAt    string `json:"played_at,omitempty" jsonschema:"RFC 3339"`
}

type MessageReceiptsOut struct {
	Status   string       `json:"status" jsonschema:"sent, delivered, read or played: the furthest any recipient got"`
	Receipts []ReceiptOut `json:"receipts" jsonschema:"one per recipient that reported anything"`
}

func (t *tools) messageReceipts(ctx context.Context, _ *mcp.CallToolRequest, in MessageReceiptsIn) (*mcp.CallToolResult, MessageReceiptsOut, error) {
	if in.ChatJID == "" || in.MessageID == "" {
		return nil, MessageReceiptsOut{}, errors.New("chat_jid and message_id are required")
	}
	chat := t.canonicalChat(ctx, in.ChatJID)
	m, err := t.store.MessageByID(ctx, chat, in.MessageID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, MessageReceiptsOut{}, fmt.Errorf("message %s not found in chat %s", in.MessageID, chat)
	}
	if err != nil {
		return nil, MessageReceiptsOut{}, err
	}
	if !m.FromMe {
		return nil, MessageReceiptsOut{}, errors.New("only messages you sent have receipts")
	}
	rs, err := t.store.Receipts(ctx, chat, in.MessageID)
	if err != nil {
		return nil, MessageReceiptsOut{}, err
	}
	from := make([]store.Message, len(rs))
	for i, r := range rs {
		from[i].SenderJID = r.ParticipantJID
	}
	names := t.senderNames(ctx, from)
	out := MessageReceiptsOut{Status: m.Status, Receipts: make([]ReceiptOut, len(rs))}
	for i, r := range rs {
		out.Receipts[i] = ReceiptOut{JID: r.ParticipantJID, Name: names[r.ParticipantJID]}
		if r.DeliveredAt > 0 {
			out.Receipts[i].DeliveredAt = formatUnix(r.DeliveredAt)
		}
		if r.ReadAt > 0 {
			out.Receipts[i].ReadAt = formatUnix(r.ReadAt)
		}
		if r.PlayedAt > 0 {
			out.Receipts[i].PlayedAt = formatUnix(r.PlayedAt)
		}
	}
	return nil, out, nil
}

type GetMediaIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"the chat's JID"`
	MessageID string `json:"message_id" jsonschema:"id of a message with media_type set"`
}

// getMedia returns the file as MCP content rather than structured output,
// so a client can render an image or save a document.
func (t *tools) getMedia(ctx context.Context, _ *mcp.CallToolRequest, in GetMediaIn) (*mcp.CallToolResult, any, error) {
	if in.ChatJID == "" || in.MessageID == "" {
		return nil, nil, errors.New("chat_jid and message_id are required")
	}
	chat := t.canonicalChat(ctx, in.ChatJID)
	m, err := t.store.MessageByID(ctx, chat, in.MessageID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, fmt.Errorf("message %s not found in chat %s", in.MessageID, chat)
	}
	if err != nil {
		return nil, nil, err
	}
	media, err := t.wa.Media(ctx, m)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: mediaContent(media, m)}, nil, nil
}

// mediaContent describes a downloaded file in a text line and attaches it:
// images (and stickers) as images, audio as audio, anything else as an
// embedded resource.
func mediaContent(media wa.Media, m store.Message) []mcp.Content {
	desc := fmt.Sprintf("%s, %s, %d bytes", media.Kind, media.MIMEType, len(media.Data))
	if media.FileName != "" {
		desc += ", file name " + strconv.Quote(media.FileName)
	}
	if m.Text != "" {
		desc += ", caption " + strconv.Quote(m.Text)
	}
	out := []mcp.Content{&mcp.TextContent{Text: desc}}
	switch media.Kind {
	case "image", "sticker":
		out = append(out, &mcp.ImageContent{Data: media.Data, MIMEType: media.MIMEType})
	case "audio":
		out = append(out, &mcp.AudioContent{Data: media.Data, MIMEType: media.MIMEType})
	default:
		out = append(out, &mcp.EmbeddedResource{Resource: &mcp.ResourceContents{
			URI:      "whatsapp://media/" + m.ChatJID + "/" + m.MessageID,
			MIMEType: media.MIMEType,
			Blob:     media.Data,
		}})
	}
	return out
}

// callsDefaultLimit is the default number of calls list_calls returns.
const callsDefaultLimit = 20

type CallOut struct {
	CallID     string `json:"call_id"`
	Caller     string `json:"caller"`
	CallerName string `json:"caller_name,omitempty"`
	Chat       string `json:"chat,omitempty" jsonschema:"the group, for group calls"`
	ChatName   string `json:"chat_name,omitempty"`
	Video      bool   `json:"video"`
	Group      bool   `json:"group"`
	StartedAt  string `json:"started_at" jsonschema:"RFC 3339"`
	EndedAt    string `json:"ended_at,omitempty" jsonschema:"RFC 3339"`
	Status     string `json:"status" jsonschema:"ringing, answered, rejected or missed"`
}

type ListCallsIn struct {
	Since string `json:"since,omitempty" jsonschema:"RFC 3339; only calls that started at or after this time"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum results (default 20, max 500)"`
}

type ListCallsOut struct {
	Calls []CallOut `json:"calls"`
}

func (t *tools) listCalls(ctx context.Context, _ *mcp.CallToolRequest, in ListCallsIn) (*mcp.CallToolResult, ListCallsOut, error) {
	var since time.Time
	if in.Since != "" {
		var err error
		if since, err = time.Parse(time.RFC3339, in.Since); err != nil {
			return nil, ListCallsOut{}, fmt.Errorf("since: want RFC 3339: %w", err)
		}
	}
	limit := in.Limit
	if limit <= 0 {
		limit = callsDefaultLimit
	}
	calls, err := t.store.ListCalls(ctx, since, limit)
	if err != nil {
		return nil, ListCallsOut{}, err
	}
	return nil, ListCallsOut{Calls: t.toCalls(ctx, calls)}, nil
}

// toCalls names callers with one contacts lookup and groups from the archive.
func (t *tools) toCalls(ctx context.Context, calls []store.Call) []CallOut {
	callers := make([]store.Message, len(calls))
	var groups []string
	for i, c := range calls {
		callers[i] = store.Message{SenderJID: c.CallerJID}
		if c.Group {
			groups = append(groups, c.ChatJID)
		}
	}
	names := t.senderNames(ctx, callers)
	chats, err := t.store.ChatsByJID(ctx, groups)
	if err != nil {
		chats = nil // names are optional; the calls are not
	}
	out := make([]CallOut, len(calls))
	for i, c := range calls {
		out[i] = CallOut{
			CallID: c.CallID, Caller: c.CallerJID, CallerName: names[c.CallerJID],
			Video: c.Video, Group: c.Group, StartedAt: formatUnix(c.StartedAt), Status: c.Status,
		}
		if c.Group {
			out[i].Chat, out[i].ChatName = c.ChatJID, chats[c.ChatJID].Name
		}
		if c.EndedAt > 0 {
			out[i].EndedAt = formatUnix(c.EndedAt)
		}
	}
	return out
}

type RejectCallIn struct {
	CallID string `json:"call_id" jsonschema:"from list_calls"`
}

func (t *tools) rejectCall(ctx context.Context, _ *mcp.CallToolRequest, in RejectCallIn) (*mcp.CallToolResult, CallOut, error) {
	if in.CallID == "" {
		return nil, CallOut{}, errors.New("call_id is required")
	}
	call, err := t.store.CallByID(ctx, in.CallID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, CallOut{}, fmt.Errorf("unknown call %s; see list_calls", in.CallID)
	}
	if err != nil {
		return nil, CallOut{}, err
	}
	if _, err := t.policy.checkRecipient(ctx, t.wa, call.CallerJID, t.wa.Status().JID); err != nil {
		return nil, CallOut{}, err
	}
	rejected, err := t.wa.RejectCall(ctx, in.CallID)
	if err != nil {
		return nil, CallOut{}, err
	}
	return nil, t.toCalls(ctx, []store.Call{rejected})[0], nil
}

type SearchContactsIn struct {
	Query string `json:"query" jsonschema:"case-insensitive substring of name, push name or phone number"`
}

type SearchContactsOut struct {
	Contacts []wa.Contact `json:"contacts"`
}

func (t *tools) searchContacts(ctx context.Context, _ *mcp.CallToolRequest, in SearchContactsIn) (*mcp.CallToolResult, SearchContactsOut, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, SearchContactsOut{}, errors.New("query is required")
	}
	contacts, err := t.wa.Contacts(ctx, in.Query)
	if err != nil {
		return nil, SearchContactsOut{}, err
	}
	return nil, SearchContactsOut{Contacts: contacts}, nil
}

// checkNumberMax caps a lookup: many numbers at once look like enumeration.
const checkNumberMax = 10

type CheckNumberIn struct {
	Phones []string `json:"phones" jsonschema:"numbers with a country code, such as +31612345678 or 0031612345678 (at most 10)"`
}

type CheckNumberOut struct {
	Numbers []wa.NumberCheck `json:"numbers" jsonschema:"one entry per distinct number, in the order given"`
}

// checkNumber sends nothing, but it does reach WhatsApp and reveals who is
// registered. So every input is validated before the lookup, and when an
// allowlist is set only the numbers on it (and the account's own) may be
// looked up: the list is the operator's limit on whom the agent deals with.
func (t *tools) checkNumber(ctx context.Context, _ *mcp.CallToolRequest, in CheckNumberIn) (*mcp.CallToolResult, CheckNumberOut, error) {
	if len(in.Phones) == 0 {
		return nil, CheckNumberOut{}, errors.New("phones is required")
	}
	if len(in.Phones) > checkNumberMax {
		return nil, CheckNumberOut{}, fmt.Errorf("at most %d numbers per call", checkNumberMax)
	}
	phones := make([]string, 0, len(in.Phones))
	for _, p := range in.Phones {
		n, err := wa.NormalizePhone(p)
		if err != nil {
			return nil, CheckNumberOut{}, err
		}
		if _, err := t.policy.checkRecipient(ctx, t.wa, n, t.wa.Status().JID); err != nil {
			return nil, CheckNumberOut{}, err
		}
		if !slices.Contains(phones, n) {
			phones = append(phones, n)
		}
	}
	checks, err := t.wa.CheckNumbers(ctx, phones)
	if err != nil {
		return nil, CheckNumberOut{}, err
	}
	return nil, CheckNumberOut{Numbers: checks}, nil
}

type GroupInfoIn struct {
	ChatJID string `json:"chat_jid" jsonschema:"the group's JID, ending in @g.us"`
}

type GroupInfoOut struct {
	Group wa.Group `json:"group"`
}

// groupInfo sends nothing, so it needs no policy.checkRecipient; the JID is
// checked before WhatsApp is asked.
func (t *tools) groupInfo(ctx context.Context, _ *mcp.CallToolRequest, in GroupInfoIn) (*mcp.CallToolResult, GroupInfoOut, error) {
	if in.ChatJID == "" {
		return nil, GroupInfoOut{}, errors.New("chat_jid is required")
	}
	jid, err := types.ParseJID(in.ChatJID)
	if err != nil || jid.Server != types.GroupServer {
		return nil, GroupInfoOut{}, fmt.Errorf("%q is not a group JID: want one ending in @g.us, from list_chats", in.ChatJID)
	}
	g, err := t.wa.GroupInfo(ctx, jid)
	if err != nil {
		return nil, GroupInfoOut{}, err
	}
	return nil, GroupInfoOut{Group: g}, nil
}

type ContactInfoIn struct {
	JID string `json:"jid" jsonschema:"a JID (from list_chats or search_contacts) or an international phone number such as +31612345678"`
}

type ContactInfoOut struct {
	Contact wa.ContactInfo `json:"contact"`
}

// contactInfo looks a person up, so like check_number it honours the
// recipient allowlist even though it sends nothing.
func (t *tools) contactInfo(ctx context.Context, _ *mcp.CallToolRequest, in ContactInfoIn) (*mcp.CallToolResult, ContactInfoOut, error) {
	if in.JID == "" {
		return nil, ContactInfoOut{}, errors.New("jid is required")
	}
	jid, err := t.policy.checkRecipient(ctx, t.wa, in.JID, t.wa.Status().JID)
	if err != nil {
		return nil, ContactInfoOut{}, err
	}
	c, err := t.wa.ContactInfo(ctx, jid)
	if err != nil {
		return nil, ContactInfoOut{}, err
	}
	return nil, ContactInfoOut{Contact: c}, nil
}

type GetProfilePictureIn struct {
	JID     string `json:"jid" jsonschema:"a contact's or group's JID, or an international phone number"`
	Preview bool   `json:"preview,omitempty" jsonschema:"return the small thumbnail instead of the full picture"`
}

func (t *tools) getProfilePicture(ctx context.Context, _ *mcp.CallToolRequest, in GetProfilePictureIn) (*mcp.CallToolResult, any, error) {
	if in.JID == "" {
		return nil, nil, errors.New("jid is required")
	}
	jid, err := t.policy.checkRecipient(ctx, t.wa, in.JID, t.wa.Status().JID)
	if err != nil {
		return nil, nil, err
	}
	media, err := t.wa.ProfilePicture(ctx, jid, in.Preview)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: mediaContent(media, store.Message{})}, nil, nil
}

type SendMessageIn struct {
	Recipient string `json:"recipient" jsonschema:"a JID (from list_chats or search_contacts) or an international phone number such as +31612345678"`
	Text      string `json:"text" jsonschema:"the message text"`
	ReplyTo   string `json:"reply_to,omitempty" jsonschema:"id of a message in the same chat to reply to (quoted)"`
}

func (t *tools) sendMessage(ctx context.Context, _ *mcp.CallToolRequest, in SendMessageIn) (*mcp.CallToolResult, Message, error) {
	if strings.TrimSpace(in.Text) == "" {
		return nil, Message{}, errors.New("text is required")
	}
	to, err := t.policy.checkRecipient(ctx, t.wa, in.Recipient, t.wa.Status().JID)
	if err != nil {
		return nil, Message{}, err
	}
	var quoted *store.Message
	if in.ReplyTo != "" {
		q, err := t.archivedMessage(ctx, to, in.ReplyTo)
		if err != nil {
			return nil, Message{}, err
		}
		quoted = &q
	}
	m, err := t.wa.SendText(ctx, to.String(), in.Text, quoted)
	if err != nil {
		return nil, Message{}, err
	}
	return nil, t.toMessages(ctx, []store.Message{m})[0], nil
}

// archivedMessage looks up a message that a write tool acts on.
func (t *tools) archivedMessage(ctx context.Context, chat types.JID, id string) (store.Message, error) {
	m, err := t.store.MessageByID(ctx, chat.String(), id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Message{}, fmt.Errorf("message %s is not archived in chat %s; find it with list_messages, "+
			"or fetch_older_messages for older history", id, chat)
	}
	return m, err
}

type SendMediaIn struct {
	Recipient string `json:"recipient" jsonschema:"a JID or an international phone number"`
	Kind      string `json:"kind" jsonschema:"image, video, audio or document"`
	Data      string `json:"data,omitempty" jsonschema:"the file, base64-encoded; or use path"`
	Path      string `json:"path,omitempty" jsonschema:"a file in the server's media folder, relative to it"`
	Caption   string `json:"caption,omitempty" jsonschema:"text shown with an image, video or document"`
	FileName  string `json:"file_name,omitempty" jsonschema:"for documents; defaults to the name of path"`
	MIMEType  string `json:"mime_type,omitempty" jsonschema:"optional; detected from the content otherwise"`
}

func (t *tools) sendMedia(ctx context.Context, _ *mcp.CallToolRequest, in SendMediaIn) (*mcp.CallToolResult, Message, error) {
	if in.Kind == "" {
		return nil, Message{}, errors.New("kind is required: image, video, audio or document")
	}
	to, err := t.policy.checkRecipient(ctx, t.wa, in.Recipient, t.wa.Status().JID)
	if err != nil {
		return nil, Message{}, err
	}
	var data []byte
	fileName := in.FileName
	switch {
	case in.Data != "" && in.Path != "":
		return nil, Message{}, errors.New("pass either data or path, not both")
	case in.Path != "":
		if data, err = t.readMediaFile(in.Path); err != nil {
			return nil, Message{}, err
		}
		if fileName == "" {
			fileName = filepath.Base(in.Path)
		}
	case in.Data != "":
		if data, err = decodeBase64(in.Data); err != nil {
			return nil, Message{}, err
		}
	default:
		return nil, Message{}, errors.New("the file is required: pass data (base64) or path")
	}
	if len(data) > wa.MaxMediaBytes {
		return nil, Message{}, fmt.Errorf("file is over the %d MB limit", wa.MaxMediaBytes>>20)
	}
	m, err := t.wa.SendMedia(ctx, to.String(), wa.OutgoingMedia{
		Kind: in.Kind, Data: data, MIMEType: in.MIMEType, Caption: in.Caption, FileName: fileName,
	})
	if err != nil {
		return nil, Message{}, err
	}
	return nil, t.toMessages(ctx, []store.Message{m})[0], nil
}

// decodeBase64 accepts standard base64, padded or not.
func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if data, err := base64.StdEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	data, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return nil, errors.New("data is not valid base64")
	}
	return data, nil
}

// readMediaFile reads a file from the media folder, refusing anything outside it.
func (t *tools) readMediaFile(path string) ([]byte, error) {
	if t.mediaRoot == nil {
		return nil, errors.New("reading files is disabled on this server (no -media-dir); pass the file as data instead")
	}
	f, err := t.mediaRoot.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %q in the media folder: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, wa.MaxMediaBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	return data, nil
}

type ReactIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"the chat's JID"`
	MessageID string `json:"message_id" jsonschema:"id of the message to react to"`
	Emoji     string `json:"emoji" jsonschema:"a single emoji, or empty to remove your reaction"`
}

type ReactOut struct {
	ChatJID   string `json:"chat_jid"`
	MessageID string `json:"message_id"`
	Removed   bool   `json:"removed" jsonschema:"true if the reaction was removed"`
}

// maxEmojiLen bounds the reaction string: an emoji with modifiers and
// joiners, not text.
const maxEmojiLen = 32

func (t *tools) react(ctx context.Context, _ *mcp.CallToolRequest, in ReactIn) (*mcp.CallToolResult, ReactOut, error) {
	if in.ChatJID == "" || in.MessageID == "" {
		return nil, ReactOut{}, errors.New("chat_jid and message_id are required")
	}
	if len(in.Emoji) > maxEmojiLen || strings.ContainsAny(in.Emoji, " \t\n") {
		return nil, ReactOut{}, errors.New("emoji must be a single emoji (or empty to remove)")
	}
	chat, err := t.policy.checkRecipient(ctx, t.wa, in.ChatJID, t.wa.Status().JID)
	if err != nil {
		return nil, ReactOut{}, err
	}
	target, err := t.archivedMessage(ctx, chat, in.MessageID)
	if err != nil {
		return nil, ReactOut{}, err
	}
	if err := t.wa.React(ctx, chat, target, in.Emoji); err != nil {
		return nil, ReactOut{}, err
	}
	return nil, ReactOut{ChatJID: chat.String(), MessageID: in.MessageID, Removed: in.Emoji == ""}, nil
}

// ownMessage looks up an archived message the account sent itself, for a
// tool that may only act on those; verb says what the tool does with it.
func (t *tools) ownMessage(ctx context.Context, chat types.JID, id, verb string) (store.Message, error) {
	m, err := t.archivedMessage(ctx, chat, id)
	if err != nil {
		return store.Message{}, err
	}
	if !m.FromMe {
		return store.Message{}, fmt.Errorf("message %s was not sent by you; only your own messages can be %s", id, verb)
	}
	return m, nil
}

type EditMessageIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"the chat's JID"`
	MessageID string `json:"message_id" jsonschema:"id of your own message to edit"`
	Text      string `json:"text" jsonschema:"the new text, or the new caption of a photo, video or document"`
}

func (t *tools) editMessage(ctx context.Context, _ *mcp.CallToolRequest, in EditMessageIn) (*mcp.CallToolResult, Message, error) {
	if in.ChatJID == "" || in.MessageID == "" {
		return nil, Message{}, errors.New("chat_jid and message_id are required")
	}
	if strings.TrimSpace(in.Text) == "" {
		return nil, Message{}, errors.New("text is required")
	}
	chat, err := t.policy.checkRecipient(ctx, t.wa, in.ChatJID, t.wa.Status().JID)
	if err != nil {
		return nil, Message{}, err
	}
	target, err := t.ownMessage(ctx, chat, in.MessageID, "edited")
	if err != nil {
		return nil, Message{}, err
	}
	m, err := t.wa.EditMessage(ctx, chat, target, in.Text)
	if err != nil {
		return nil, Message{}, err
	}
	return nil, t.toMessages(ctx, []store.Message{m})[0], nil
}

type DeleteMessageIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"the chat's JID"`
	MessageID string `json:"message_id" jsonschema:"id of your own message to delete"`
}

type DeleteMessageOut struct {
	ChatJID   string `json:"chat_jid"`
	MessageID string `json:"message_id"`
	Deleted   bool   `json:"deleted"`
}

func (t *tools) deleteMessage(ctx context.Context, _ *mcp.CallToolRequest, in DeleteMessageIn) (*mcp.CallToolResult, DeleteMessageOut, error) {
	if in.ChatJID == "" || in.MessageID == "" {
		return nil, DeleteMessageOut{}, errors.New("chat_jid and message_id are required")
	}
	chat, err := t.policy.checkRecipient(ctx, t.wa, in.ChatJID, t.wa.Status().JID)
	if err != nil {
		return nil, DeleteMessageOut{}, err
	}
	target, err := t.ownMessage(ctx, chat, in.MessageID, "deleted")
	if err != nil {
		return nil, DeleteMessageOut{}, err
	}
	if err := t.wa.DeleteMessage(ctx, chat, target); err != nil {
		return nil, DeleteMessageOut{}, err
	}
	return nil, DeleteMessageOut{ChatJID: chat.String(), MessageID: in.MessageID, Deleted: true}, nil
}

type MarkReadIn struct {
	ChatJID string `json:"chat_jid" jsonschema:"the chat's JID"`
}

type MarkReadOut struct {
	ChatJID string `json:"chat_jid"`
}

func (t *tools) markRead(ctx context.Context, _ *mcp.CallToolRequest, in MarkReadIn) (*mcp.CallToolResult, MarkReadOut, error) {
	if in.ChatJID == "" {
		return nil, MarkReadOut{}, errors.New("chat_jid is required")
	}
	chat, err := t.policy.checkRecipient(ctx, t.wa, in.ChatJID, t.wa.Status().JID)
	if err != nil {
		return nil, MarkReadOut{}, err
	}
	if err := t.wa.MarkChatRead(ctx, chat); err != nil {
		return nil, MarkReadOut{}, err
	}
	return nil, MarkReadOut{ChatJID: chat.String()}, nil
}

type SetChatStateIn struct {
	ChatJID  string `json:"chat_jid" jsonschema:"the chat's JID"`
	Archived *bool  `json:"archived,omitempty"`
	Pinned   *bool  `json:"pinned,omitempty"`
	Mute     string `json:"mute,omitempty" jsonschema:"off, forever, or a duration such as 8h or 168h"`
}

func (t *tools) setChatState(ctx context.Context, _ *mcp.CallToolRequest, in SetChatStateIn) (*mcp.CallToolResult, Chat, error) {
	if in.ChatJID == "" {
		return nil, Chat{}, errors.New("chat_jid is required")
	}
	f := store.ChatFlags{Archived: in.Archived, Pinned: in.Pinned}
	switch in.Mute {
	case "":
	case "off":
		f.MutedUntil = ptr(int64(0))
	case "forever":
		f.MutedUntil = ptr(int64(-1))
	default:
		d, err := time.ParseDuration(in.Mute)
		if err != nil || d <= 0 {
			return nil, Chat{}, fmt.Errorf("mute: want off, forever or a duration such as 8h, not %q", in.Mute)
		}
		f.MutedUntil = ptr(time.Now().Add(d).Unix())
	}
	if f.Archived == nil && f.Pinned == nil && f.MutedUntil == nil {
		return nil, Chat{}, errors.New("set at least one of archived, pinned or mute")
	}
	chat, err := t.policy.checkRecipient(ctx, t.wa, in.ChatJID, t.wa.Status().JID)
	if err != nil {
		return nil, Chat{}, err
	}
	if err := t.wa.SetChatState(ctx, chat, f); err != nil {
		return nil, Chat{}, err
	}
	if f.Archived != nil && *f.Archived {
		f.Pinned = ptr(false) // WhatsApp unpins an archived chat
	}
	if err := t.store.SetChatFlags(ctx, chat.String(), f); err != nil {
		return nil, Chat{}, err
	}
	chats, err := t.store.ChatsByJID(ctx, []string{chat.String()})
	if err != nil {
		return nil, Chat{}, err
	}
	c := chats[chat.String()]
	c.JID = chat.String()
	return nil, toChat(c), nil
}

// canonicalChat maps a chat JID given by a client to the archive's form, so
// an @lid reference finds a chat archived under the phone number. Input that
// isn't a JID is passed through; it simply won't match anything.
func (t *tools) canonicalChat(ctx context.Context, chatJID string) string {
	jid, err := types.ParseJID(chatJID)
	if err != nil {
		return chatJID
	}
	return t.wa.Canonical(ctx, jid).String()
}

func ptr[T any](v T) *T { return &v }
