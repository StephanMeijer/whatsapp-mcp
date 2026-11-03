// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

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
