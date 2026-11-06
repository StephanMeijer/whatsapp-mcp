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

type SetDisappearingTimerIn struct {
	ChatJID string `json:"chat_jid" jsonschema:"the chat's JID"`
	Timer   string `json:"timer" jsonschema:"off, 24h, 7d or 90d"`
}

const day = 24 * time.Hour

// disappearingTimers are the timers WhatsApp offers; it refuses others.
var disappearingTimers = map[string]time.Duration{"off": 0, "24h": day, "7d": 7 * day, "90d": 90 * day}

// formatTimer shows a timer in seconds the way set_disappearing_timer takes
// it: 24h, 7d or 90d, and any other value as a Go duration.
func formatTimer(secs int64) string {
	d := time.Duration(secs) * time.Second
	for name, v := range disappearingTimers {
		if v == d && v > 0 {
			return name
		}
	}
	return d.String()
}

func (t *tools) setDisappearingTimer(ctx context.Context, _ *mcp.CallToolRequest, in SetDisappearingTimerIn) (*mcp.CallToolResult, Chat, error) {
	if in.ChatJID == "" {
		return nil, Chat{}, errors.New("chat_jid is required")
	}
	timer, ok := disappearingTimers[in.Timer]
	if !ok {
		return nil, Chat{}, fmt.Errorf("timer: want off, 24h, 7d or 90d, not %q", in.Timer)
	}
	chat, err := t.policy.checkRecipient(ctx, t.wa, in.ChatJID)
	if err != nil {
		return nil, Chat{}, err
	}
	if err := t.wa.SetDisappearingTimer(ctx, chat, timer); err != nil {
		return nil, Chat{}, err
	}
	secs := int64(timer / time.Second)
	if err := t.store.SetChatFlags(ctx, chat.String(), store.ChatFlags{DisappearingSeconds: &secs}); err != nil {
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
