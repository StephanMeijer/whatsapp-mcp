// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

type SendLocationIn struct {
	Recipient string  `json:"recipient" jsonschema:"a JID or an international phone number"`
	Latitude  float64 `json:"latitude" jsonschema:"degrees, -90 to 90"`
	Longitude float64 `json:"longitude" jsonschema:"degrees, -180 to 180"`
	Name      string  `json:"name,omitempty" jsonschema:"the place's name, shown on the pin"`
	Address   string  `json:"address,omitempty" jsonschema:"the place's address, shown under the name"`
}

func (t *tools) sendLocation(ctx context.Context, _ *mcp.CallToolRequest, in SendLocationIn) (*mcp.CallToolResult, Message, error) {
	to, err := t.policy.checkRecipient(ctx, t.wa, in.Recipient)
	if err != nil {
		return nil, Message{}, err
	}
	m, err := t.wa.SendLocation(ctx, to.String(), in.Latitude, in.Longitude, in.Name, in.Address)
	if err != nil {
		return nil, Message{}, err
	}
	return nil, t.toMessages(ctx, []store.Message{m})[0], nil
}
