// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// chatFlagsFor maps an archive, pin or mute change from the phone.
func chatFlagsFor(evt any) (types.JID, store.ChatFlags, bool) {
	switch e := evt.(type) {
	case *events.Archive:
		if e.Action == nil {
			break
		}
		return e.JID, store.ChatFlags{Archived: proto.Bool(e.Action.GetArchived())}, true
	case *events.Pin:
		if e.Action == nil {
			break
		}
		return e.JID, store.ChatFlags{Pinned: proto.Bool(e.Action.GetPinned())}, true
	case *events.Mute:
		if e.Action == nil {
			break
		}
		var until int64
		if e.Action.GetMuted() {
			until = -1
			if ms := e.Action.GetMuteEndTimestamp(); ms > 0 {
				until = ms / 1000
			}
		}
		return e.JID, store.ChatFlags{MutedUntil: &until}, true
	}
	return types.JID{}, store.ChatFlags{}, false
}

// onChatState mirrors an archive, pin or mute change into the archive.
// shortcut: a device paired before this version does not replay old app
// state, so flags are known from the next change on; a one-off
// FetchAppState(fullSync) on connect would backfill when someone needs it.
func (c *Client) onChatState(evt any) {
	jid, f, ok := chatFlagsFor(evt)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
	defer cancel()
	chat := c.Canonical(ctx, jid).String()
	if err := c.store.SetChatFlags(ctx, chat, f); err != nil {
		c.log.Error("record chat state", "err", err, "chat", chat)
		return
	}
	c.log.Debug("chat state", "chat", chat, "event", fmt.Sprintf("%T", evt))
}

// SetChatState archives, pins or mutes a chat on WhatsApp, for each field
// that is set; the phone follows. The caller records the result.
func (c *Client) SetChatState(ctx context.Context, chat types.JID, f store.ChatFlags) error {
	if !c.cli.IsLoggedIn() {
		return errors.New("not logged in to WhatsApp")
	}
	chat = chat.ToNonAD()
	if f.Archived != nil {
		if err := c.cli.SendAppState(ctx, appstate.BuildArchive(chat, *f.Archived, time.Time{}, nil)); err != nil {
			return fmt.Errorf("archive chat: %w", err)
		}
	}
	// Archiving unpins by itself (BuildArchive includes the unpin).
	if f.Pinned != nil && (f.Archived == nil || !*f.Archived) {
		if err := c.cli.SendAppState(ctx, appstate.BuildPin(chat, *f.Pinned)); err != nil {
			return fmt.Errorf("pin chat: %w", err)
		}
	}
	if f.MutedUntil != nil {
		var end *int64
		if *f.MutedUntil > 0 {
			end = proto.Int64(*f.MutedUntil * 1000)
		}
		if err := c.cli.SendAppState(ctx, appstate.BuildMuteAbs(chat, *f.MutedUntil != 0, end)); err != nil {
			return fmt.Errorf("mute chat: %w", err)
		}
	}
	c.log.Info("set chat state", "chat", chat.String())
	return nil
}
