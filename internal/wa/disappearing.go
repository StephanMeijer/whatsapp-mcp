// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// ephemeralSetting reports the timer an EPHEMERAL_SETTING protocol message
// sets, in seconds (0 turns disappearing messages off).
func ephemeralSetting(m *waE2E.Message) (int64, bool) {
	pm := m.GetProtocolMessage()
	if pm.GetType() != waE2E.ProtocolMessage_EPHEMERAL_SETTING {
		return 0, false
	}
	return int64(pm.GetEphemeralExpiration()), true
}

// groupTimer is a group's disappearing-message timer in seconds, 0 when off.
func groupTimer(e types.GroupEphemeral) int64 {
	if !e.IsEphemeral {
		return 0
	}
	return int64(e.DisappearingTimer)
}

// recordTimer stores a chat's disappearing-message timer. A failure is
// logged: the timer is a display detail, not worth failing the event for.
func (c *Client) recordTimer(ctx context.Context, chat string, secs int64) {
	if err := c.store.SetChatFlags(ctx, chat, store.ChatFlags{DisappearingSeconds: &secs}); err != nil {
		c.log.Error("record disappearing timer", "err", err, "chat", chat)
	}
}

// SetDisappearingTimer turns disappearing messages on for a chat with the
// given timer, or off with 0. In a direct chat WhatsApp announces the change
// in the chat; recording it in the archive is the caller's job.
func (c *Client) SetDisappearingTimer(ctx context.Context, chat types.JID, timer time.Duration) error {
	if !c.cli.IsLoggedIn() {
		return errNotLoggedIn
	}
	return c.cli.SetDisappearingTimer(ctx, chat, timer, time.Time{})
}
