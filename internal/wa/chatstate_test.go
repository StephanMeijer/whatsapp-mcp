// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"strconv"
	"testing"

	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestChatFlagsFor(t *testing.T) {
	jid := types.NewJID("31600000001", types.DefaultUserServer)
	str := func(f store.ChatFlags) string {
		out := ""
		if f.Archived != nil {
			out += "archived=" + strconv.FormatBool(*f.Archived) + " "
		}
		if f.Pinned != nil {
			out += "pinned=" + strconv.FormatBool(*f.Pinned) + " "
		}
		if f.MutedUntil != nil {
			out += "muted=" + strconv.FormatInt(*f.MutedUntil, 10)
		}
		return out
	}
	tests := []struct {
		name string
		evt  any
		want string // empty: ignored
	}{
		{"archive", &events.Archive{JID: jid, Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(true)}}, "archived=true "},
		{"unarchive", &events.Archive{JID: jid, Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(false)}}, "archived=false "},
		{"pin", &events.Pin{JID: jid, Action: &waSyncAction.PinAction{Pinned: proto.Bool(true)}}, "pinned=true "},
		{"unmute", &events.Mute{JID: jid, Action: &waSyncAction.MuteAction{Muted: proto.Bool(false)}}, "muted=0"},
		{"mute forever", &events.Mute{JID: jid, Action: &waSyncAction.MuteAction{Muted: proto.Bool(true), MuteEndTimestamp: proto.Int64(-1)}}, "muted=-1"},
		{"mute until", &events.Mute{JID: jid, Action: &waSyncAction.MuteAction{Muted: proto.Bool(true), MuteEndTimestamp: proto.Int64(1_700_000_000_000)}}, "muted=1700000000"},
		{"no action", &events.Pin{JID: jid}, ""},
		{"other event", &events.Connected{}, ""},
	}
	for _, tt := range tests {
		got, f, ok := chatFlagsFor(tt.evt)
		if ok != (tt.want != "") || (ok && (got != jid || str(f) != tt.want)) {
			t.Errorf("%s: chatFlagsFor = %s %q %v, want %q", tt.name, got, str(f), ok, tt.want)
		}
	}
}
