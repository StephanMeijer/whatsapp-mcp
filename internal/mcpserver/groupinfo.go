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

type GroupInfoIn struct {
	ChatJID string `json:"chat_jid" jsonschema:"the group's JID, ending in @g.us"`
}

type GroupInfoOut struct {
	Group wa.Group `json:"group"`
}

// groupInfo sends nothing, so it needs no policy.checkRecipient; the JID is
// checked before WhatsApp is asked.
func (t *tools) groupInfo(ctx context.Context, _ *mcp.CallToolRequest, in GroupInfoIn) (*mcp.CallToolResult, GroupInfoOut, error) {
	if in.ChatJID == "" {
		return nil, GroupInfoOut{}, errors.New("chat_jid is required")
	}
	jid, err := types.ParseJID(in.ChatJID)
	if err != nil || jid.Server != types.GroupServer {
		return nil, GroupInfoOut{}, fmt.Errorf("%q is not a group JID: want one ending in @g.us, from list_chats", in.ChatJID)
	}
	g, err := t.wa.GroupInfo(ctx, jid)
	if err != nil {
		return nil, GroupInfoOut{}, err
	}
	return nil, GroupInfoOut{Group: g}, nil
}
