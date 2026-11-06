// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

type SendPollIn struct {
	Recipient string   `json:"recipient" jsonschema:"a JID or an international phone number"`
	Question  string   `json:"question"`
	Options   []string `json:"options" jsonschema:"2 to 12 different options"`
	Multiple  bool     `json:"multiple,omitempty" jsonschema:"let people choose more than one option"`
}

func (t *tools) sendPoll(ctx context.Context, _ *mcp.CallToolRequest, in SendPollIn) (*mcp.CallToolResult, Message, error) {
	to, err := t.policy.checkRecipient(ctx, t.wa, in.Recipient)
	if err != nil {
		return nil, Message{}, err
	}
	m, err := t.wa.SendPoll(ctx, to.String(), in.Question, in.Options, in.Multiple)
	if err != nil {
		return nil, Message{}, err
	}
	return nil, t.toMessages(ctx, []store.Message{m})[0], nil
}
