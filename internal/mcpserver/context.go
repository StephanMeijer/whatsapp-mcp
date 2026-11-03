// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

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
