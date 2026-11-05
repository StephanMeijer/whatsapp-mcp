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

// parseMute turns set_chat_state's mute argument into store.ChatFlags'
// MutedUntil: nil to leave it, 0 for off, -1 for forever, else Unix seconds.
func parseMute(s string, now time.Time) (*int64, error) {
	switch s {
	case "":
		return nil, nil
	case "off":
		return new(int64(0)), nil
	case "forever":
		return new(int64(-1)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return nil, fmt.Errorf("mute: want off, forever or a duration such as 8h, not %q", s)
	}
	return new(now.Add(d).Unix()), nil
}

func (t *tools) setChatState(ctx context.Context, _ *mcp.CallToolRequest, in SetChatStateIn) (*mcp.CallToolResult, Chat, error) {
	if in.ChatJID == "" {
		return nil, Chat{}, errors.New("chat_jid is required")
	}
	mutedUntil, err := parseMute(in.Mute, time.Now())
	if err != nil {
		return nil, Chat{}, err
	}
	f := store.ChatFlags{Archived: in.Archived, Pinned: in.Pinned, MutedUntil: mutedUntil}
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
