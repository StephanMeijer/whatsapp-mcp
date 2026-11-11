// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

type GetPresenceIn struct {
	JID string `json:"jid" jsonschema:"a person's JID (from list_chats or search_contacts) or an international phone number such as +31612345678"`
}

// getPresence looks a person up, so like contact_info it honours the
// recipient allowlist even though it sends them nothing.
func (t *tools) getPresence(ctx context.Context, _ *mcp.CallToolRequest, in GetPresenceIn) (*mcp.CallToolResult, wa.Presence, error) {
	if in.JID == "" {
		return nil, wa.Presence{}, errors.New("jid is required")
	}
	jid, err := t.policy.checkRecipient(ctx, t.wa, in.JID)
	if err != nil {
		return nil, wa.Presence{}, err
	}
	if jid.Server == types.GroupServer {
		return nil, wa.Presence{}, errors.New("jid: a group has no presence; give a person")
	}
	p, err := t.wa.Presence(ctx, jid)
	return nil, p, err
}

type SetTypingIn struct {
	ChatJID string `json:"chat_jid" jsonschema:"the chat's JID"`
	State   string `json:"state" jsonschema:"typing, recording or paused"`
}

type SetTypingOut struct {
	ChatJID string `json:"chat_jid"`
	State   string `json:"state"`
}

func (t *tools) setTyping(ctx context.Context, _ *mcp.CallToolRequest, in SetTypingIn) (*mcp.CallToolResult, SetTypingOut, error) {
	if in.ChatJID == "" {
		return nil, SetTypingOut{}, errors.New("chat_jid is required")
	}
	state, media := types.ChatPresenceComposing, types.ChatPresenceMediaText
	switch in.State {
	case "typing":
	case "recording":
		media = types.ChatPresenceMediaAudio
	case "paused":
		state = types.ChatPresencePaused
	default:
		return nil, SetTypingOut{}, fmt.Errorf("state: want typing, recording or paused, not %q", in.State)
	}
	chat, err := t.policy.checkRecipient(ctx, t.wa, in.ChatJID)
	if err != nil {
		return nil, SetTypingOut{}, err
	}
	if err := t.wa.SetTyping(ctx, chat, state, media); err != nil {
		return nil, SetTypingOut{}, err
	}
	return nil, SetTypingOut{ChatJID: chat.String(), State: in.State}, nil
}
