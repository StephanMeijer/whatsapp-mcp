// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

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
	chat, err := t.policy.checkRecipient(ctx, t.wa, in.ChatJID)
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
	chat, err := t.policy.checkRecipient(ctx, t.wa, in.ChatJID)
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
	chat, err := t.policy.checkRecipient(ctx, t.wa, in.ChatJID)
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
	chat, err := t.policy.checkRecipient(ctx, t.wa, in.ChatJID)
	if err != nil {
		return nil, MarkReadOut{}, err
	}
	if err := t.wa.MarkChatRead(ctx, chat); err != nil {
		return nil, MarkReadOut{}, err
	}
	return nil, MarkReadOut{ChatJID: chat.String()}, nil
}
