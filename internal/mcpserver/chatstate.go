// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

type SetChatStateIn struct {
	ChatJID  string `json:"chat_jid" jsonschema:"the chat's JID"`
	Archived *bool  `json:"archived,omitempty"`
	Pinned   *bool  `json:"pinned,omitempty"`
	Mute     string `json:"mute,omitempty" jsonschema:"off, forever, or a duration such as 8h or 168h"`
}

func (t *tools) setChatState(ctx context.Context, _ *mcp.CallToolRequest, in SetChatStateIn) (*mcp.CallToolResult, Chat, error) {
	if in.ChatJID == "" {
		return nil, Chat{}, errors.New("chat_jid is required")
	}
	f := store.ChatFlags{Archived: in.Archived, Pinned: in.Pinned}
	switch in.Mute {
	case "":
	case "off":
		f.MutedUntil = new(int64(0))
	case "forever":
		f.MutedUntil = new(int64(-1))
	default:
		d, err := time.ParseDuration(in.Mute)
		if err != nil || d <= 0 {
			return nil, Chat{}, fmt.Errorf("mute: want off, forever or a duration such as 8h, not %q", in.Mute)
		}
		f.MutedUntil = new(time.Now().Add(d).Unix())
	}
	if f.Archived == nil && f.Pinned == nil && f.MutedUntil == nil {
		return nil, Chat{}, errors.New("set at least one of archived, pinned or mute")
	}
	chat, err := t.policy.checkRecipient(ctx, t.wa, in.ChatJID)
	if err != nil {
		return nil, Chat{}, err
	}
	if err := t.wa.SetChatState(ctx, chat, f); err != nil {
		return nil, Chat{}, err
	}
	if f.Archived != nil && *f.Archived {
		f.Pinned = new(false) // WhatsApp unpins an archived chat
	}
	if err := t.store.SetChatFlags(ctx, chat.String(), f); err != nil {
		return nil, Chat{}, err
	}
	chats, err := t.store.ChatsByJID(ctx, []string{chat.String()})
	if err != nil {
		return nil, Chat{}, err
	}
	c := chats[chat.String()]
	c.JID = chat.String()
	return nil, toChat(c), nil
}
