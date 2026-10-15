// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

type tools struct {
	wa     WhatsApp
	store  *store.Store
	policy *policy
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
		Name:        "search_messages",
		Description: "Full-text search message text and captions, optionally within one chat. All words must match.",
		Annotations: readOnly,
	}, t.searchMessages)
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
		Name:        "send_message",
		Description: "Send a text message. This is delivered immediately and cannot be undone.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true)},
	}, t.sendMessage)
}

// Message is the tool-facing form of store.Message.
type Message struct {
	ChatJID   string `json:"chat_jid"`
	ID        string `json:"id"`
	Sender    string `json:"sender"`
	Timestamp string `json:"timestamp" jsonschema:"RFC 3339"`
	FromMe    bool   `json:"from_me"`
	Text      string `json:"text,omitempty"`
	MediaType string `json:"media_type,omitempty" jsonschema:"image, video, audio, document or sticker; empty for text"`
}

func toMessages(msgs []store.Message) []Message {
	out := make([]Message, len(msgs))
	for i, m := range msgs {
		out[i] = Message{
			ChatJID:   m.ChatJID,
			ID:        m.MessageID,
			Sender:    m.SenderJID,
			Timestamp: formatUnix(m.Timestamp),
			FromMe:    m.FromMe,
			Text:      m.Text,
			MediaType: m.MediaType,
		}
	}
	return out
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
	msgs, err := t.store.ListMessages(ctx, in.ChatJID, before, in.Limit)
	if err != nil {
		return nil, MessagesOut{}, err
	}
	return nil, MessagesOut{Messages: toMessages(msgs)}, nil
}

type SearchMessagesIn struct {
	Query   string `json:"query" jsonschema:"words to search for"`
	ChatJID string `json:"chat_jid,omitempty" jsonschema:"restrict to this chat"`
	Limit   int    `json:"limit,omitempty" jsonschema:"maximum results (default 50, max 500)"`
}

func (t *tools) searchMessages(ctx context.Context, _ *mcp.CallToolRequest, in SearchMessagesIn) (*mcp.CallToolResult, MessagesOut, error) {
	msgs, err := t.store.SearchMessages(ctx, in.Query, in.ChatJID, in.Limit)
	if err != nil {
		return nil, MessagesOut{}, err
	}
	return nil, MessagesOut{Messages: toMessages(msgs)}, nil
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
}

func (t *tools) sendMessage(ctx context.Context, _ *mcp.CallToolRequest, in SendMessageIn) (*mcp.CallToolResult, Message, error) {
	if strings.TrimSpace(in.Text) == "" {
		return nil, Message{}, errors.New("text is required")
	}
	to, err := t.policy.checkRecipient(in.Recipient, t.wa.Status().JID)
	if err != nil {
		return nil, Message{}, err
	}
	m, err := t.wa.SendText(ctx, to.String(), in.Text)
	if err != nil {
		return nil, Message{}, err
	}
	return nil, toMessages([]store.Message{m})[0], nil
}

func ptr[T any](v T) *T { return &v }
