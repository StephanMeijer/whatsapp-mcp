// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

type SendContactIn struct {
	Recipient string `json:"recipient" jsonschema:"a JID or an international phone number"`
	Name      string `json:"name" jsonschema:"the contact's name, as shown on the card"`
	Phone     string `json:"phone" jsonschema:"the contact's number with country code, such as +31612345678"`
}

func (t *tools) sendContact(ctx context.Context, _ *mcp.CallToolRequest, in SendContactIn) (*mcp.CallToolResult, Message, error) {
	to, err := t.policy.checkRecipient(ctx, t.wa, in.Recipient)
	if err != nil {
		return nil, Message{}, err
	}
	m, err := t.wa.SendContact(ctx, to.String(), in.Name, in.Phone)
	if err != nil {
		return nil, Message{}, err
	}
	return nil, t.toMessages(ctx, []store.Message{m})[0], nil
}
