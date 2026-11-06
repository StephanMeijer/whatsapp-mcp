// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

type ForwardMessageIn struct {
	FromChatJID string `json:"from_chat_jid" jsonschema:"the chat the message is in"`
	MessageID   string `json:"message_id" jsonschema:"id of the message to forward"`
	Recipient   string `json:"recipient" jsonschema:"where to forward it: a JID or an international phone number"`
}

func (t *tools) forwardMessage(ctx context.Context, _ *mcp.CallToolRequest, in ForwardMessageIn) (*mcp.CallToolResult, Message, error) {
	if in.FromChatJID == "" || in.MessageID == "" || in.Recipient == "" {
		return nil, Message{}, errors.New("from_chat_jid, message_id and recipient are required")
	}
	to, err := t.policy.checkRecipient(ctx, t.wa, in.Recipient)
	if err != nil {
		return nil, Message{}, err
	}
	from := t.canonicalChat(ctx, in.FromChatJID)
	m, err := t.store.MessageByID(ctx, from, in.MessageID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, Message{}, fmt.Errorf("message %s not found in chat %s", in.MessageID, from)
	}
	if err != nil {
		return nil, Message{}, err
	}
	sent, err := t.wa.Forward(ctx, to.String(), m)
	if err != nil {
		return nil, Message{}, err
	}
	return nil, t.toMessages(ctx, []store.Message{sent})[0], nil
}
