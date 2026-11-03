// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

type ContactInfoIn struct {
	JID string `json:"jid" jsonschema:"a JID (from list_chats or search_contacts) or an international phone number such as +31612345678"`
}

type ContactInfoOut struct {
	Contact wa.ContactInfo `json:"contact"`
}

// contactInfo looks a person up, so like check_number it honours the
// recipient allowlist even though it sends nothing.
func (t *tools) contactInfo(ctx context.Context, _ *mcp.CallToolRequest, in ContactInfoIn) (*mcp.CallToolResult, ContactInfoOut, error) {
	if in.JID == "" {
		return nil, ContactInfoOut{}, errors.New("jid is required")
	}
	jid, err := t.policy.checkRecipient(ctx, t.wa, in.JID, t.wa.Status().JID)
	if err != nil {
		return nil, ContactInfoOut{}, err
	}
	c, err := t.wa.ContactInfo(ctx, jid)
	if err != nil {
		return nil, ContactInfoOut{}, err
	}
	return nil, ContactInfoOut{Contact: c}, nil
}

type GetProfilePictureIn struct {
	JID     string `json:"jid" jsonschema:"a contact's or group's JID, or an international phone number"`
	Preview bool   `json:"preview,omitempty" jsonschema:"return the small thumbnail instead of the full picture"`
}

func (t *tools) getProfilePicture(ctx context.Context, _ *mcp.CallToolRequest, in GetProfilePictureIn) (*mcp.CallToolResult, any, error) {
	if in.JID == "" {
		return nil, nil, errors.New("jid is required")
	}
	jid, err := t.policy.checkRecipient(ctx, t.wa, in.JID, t.wa.Status().JID)
	if err != nil {
		return nil, nil, err
	}
	media, err := t.wa.ProfilePicture(ctx, jid, in.Preview)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: mediaContent(media, store.Message{})}, nil, nil
}
