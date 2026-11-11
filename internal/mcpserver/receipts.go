// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

type MessageReceiptsIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"the chat's JID or an international phone number such as +31612345678"`
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
	chat, err := t.canonicalChat(ctx, in.ChatJID)
	if err != nil {
		return nil, MessageReceiptsOut{}, fmt.Errorf("chat_jid: %w", err)
	}
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
	from := make([]string, len(rs))
	for i, r := range rs {
		from[i] = r.ParticipantJID
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
