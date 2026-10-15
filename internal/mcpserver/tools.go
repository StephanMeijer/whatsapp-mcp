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
		Name:        "list_chats",
		Description: "List chats, most recently active first, optionally filtered by name or JID.",
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
		Name:        "search_contacts",
		Description: "Search the address book by name, push name or phone number.",
		Annotations: readOnly,
	}, t.searchContacts)

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
		Name: "react",
		Description: "React to a message with an emoji, visible to everyone in the chat. " +
			"An empty emoji removes your reaction.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true), IdempotentHint: true},
	}, t.react)
	mcp.AddTool(s, &mcp.Tool{
		Name: "mark_read",
		Description: "Mark a chat as read: senders of its latest messages see blue ticks, and the unread " +
			"badge clears on all your devices.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true), IdempotentHint: true},
	}, t.markRead)
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
	Query string `json:"query,omitempty" jsonschema:"case-insensitive substring of the chat name or JID"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum results (default 50, max 500)"`
}

type Chat struct {
	JID           string `json:"jid"`
	Name          string `json:"name,omitempty"`
	LastMessageAt string `json:"last_message_at,omitempty" jsonschema:"RFC 3339"`
}

type ListChatsOut struct {
	Chats []Chat `json:"chats"`
}

func (t *tools) listChats(ctx context.Context, _ *mcp.CallToolRequest, in ListChatsIn) (*mcp.CallToolResult, ListChatsOut, error) {
	chats, err := t.store.ListChats(ctx, in.Query, in.Limit)
	if err != nil {
		return nil, ListChatsOut{}, err
	}
	out := ListChatsOut{Chats: make([]Chat, len(chats))}
	for i, c := range chats {
		out.Chats[i] = Chat{JID: c.JID, Name: c.Name}
		if c.LastMessageAt > 0 {
			out.Chats[i].LastMessageAt = formatUnix(c.LastMessageAt)
		}
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
