// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// recentDefaultLimit is higher than other tools' default: a catch-up spans many chats.
const recentDefaultLimit = 200

type RecentMessagesIn struct {
	Since         string `json:"since" jsonschema:"RFC 3339; messages at or after this time"`
	Until         string `json:"until,omitempty" jsonschema:"RFC 3339; only messages before this time"`
	ChatJID       string `json:"chat_jid,omitempty" jsonschema:"restrict to this chat"`
	IncludeFromMe bool   `json:"include_from_me,omitempty" jsonschema:"also return messages you sent (default false)"`
	Limit         int    `json:"limit,omitempty" jsonschema:"maximum messages in total (default 200, max 500)"`
}

type ChatMessages struct {
	JID      string    `json:"jid"`
	Name     string    `json:"name,omitempty"`
	Messages []Message `json:"messages" jsonschema:"oldest first"`
}

type RecentMessagesOut struct {
	Chats     []ChatMessages `json:"chats" jsonschema:"most recently active chat first"`
	Total     int            `json:"total" jsonschema:"number of messages returned"`
	Truncated bool           `json:"truncated" jsonschema:"true if older messages in the window were left out"`
}

func (t *tools) recentMessages(ctx context.Context, _ *mcp.CallToolRequest, in RecentMessagesIn) (*mcp.CallToolResult, RecentMessagesOut, error) {
	if in.Since == "" {
		return nil, RecentMessagesOut{}, errors.New("since is required, e.g. 2026-01-02T00:00:00Z")
	}
	since, err := time.Parse(time.RFC3339, in.Since)
	if err != nil {
		return nil, RecentMessagesOut{}, fmt.Errorf("since: want RFC 3339, e.g. 2026-01-02T00:00:00Z: %w", err)
	}
	var until time.Time
	if in.Until != "" {
		if until, err = time.Parse(time.RFC3339, in.Until); err != nil {
			return nil, RecentMessagesOut{}, fmt.Errorf("until: want RFC 3339: %w", err)
		}
		if !until.After(since) {
			return nil, RecentMessagesOut{}, errors.New("until must be after since")
		}
	}
	limit := in.Limit
	if limit <= 0 {
		limit = recentDefaultLimit
	}
	chat := t.canonicalChat(ctx, in.ChatJID)

	msgs, truncated, err := t.store.RecentMessages(ctx, store.RecentQuery{
		Since: since, Until: until, ChatJID: chat, IncludeFromMe: in.IncludeFromMe, Limit: limit,
	})
	if err != nil {
		return nil, RecentMessagesOut{}, err
	}

	// msgs is newest first, so chats appear in order of latest activity.
	out := RecentMessagesOut{Chats: []ChatMessages{}, Total: len(msgs), Truncated: truncated}
	index := map[string]int{}
	for _, m := range t.toMessages(ctx, msgs) {
		i, ok := index[m.ChatJID]
		if !ok {
			i = len(out.Chats)
			index[m.ChatJID] = i
			out.Chats = append(out.Chats, ChatMessages{JID: m.ChatJID})
		}
		out.Chats[i].Messages = append(out.Chats[i].Messages, m)
	}
	jids := make([]string, len(out.Chats))
	for i := range out.Chats {
		slices.Reverse(out.Chats[i].Messages) // oldest first within a chat
		jids[i] = out.Chats[i].JID
	}
	chats, err := t.store.ChatsByJID(ctx, jids)
	if err != nil {
		return nil, RecentMessagesOut{}, err
	}
	for i := range out.Chats {
		out.Chats[i].Name = chats[out.Chats[i].JID].Name
	}
	return nil, out, nil
}
