// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"slices"
	"strings"
	"testing"
)

func TestSetDisappearingTimer(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, chatStateStore(t), Options{AllowRecipients: []string{alice}})

	set := func(args map[string]any) Chat {
		t.Helper()
		var out Chat
		if res := call(t, cs, "set_disappearing_timer", args, &out); res.IsError {
			t.Fatalf("set_disappearing_timer(%v): %v", args, res.Content)
		}
		return out
	}
	if out := set(map[string]any{"chat_jid": alice, "timer": "7d"}); out.DisappearingTimer != "7d" || out.Name != "Alice" {
		t.Errorf("7d = %+v", out)
	}
	var chats ListChatsOut
	call(t, cs, "list_chats", map[string]any{"query": "alice"}, &chats)
	if len(chats.Chats) != 1 || chats.Chats[0].DisappearingTimer != "7d" {
		t.Errorf("list_chats = %+v, want Alice's timer shown", chats.Chats)
	}
	if out := set(map[string]any{"chat_jid": alice, "timer": "off"}); out.DisappearingTimer != "" {
		t.Errorf("off = %+v", out)
	}

	for _, args := range []map[string]any{
		{"chat_jid": alice, "timer": "1w"}, // not a timer WhatsApp offers
		{"chat_jid": alice},                // no timer
		{"timer": "24h"},                   // no chat
		{"chat_jid": bob, "timer": "24h"},  // not in the allowlist
	} {
		if res := call(t, cs, "set_disappearing_timer", args, nil); !res.IsError {
			t.Errorf("set_disappearing_timer(%v) succeeded, want an error", args)
		}
	}
	want := []string{"timer " + alice + " 168h0m0s", "timer " + alice + " 0s"}
	if !slices.Equal(fake.sent, want) {
		t.Errorf("sent = %v, want %v", fake.sent, want)
	}
}

func TestFormatTimer(t *testing.T) {
	for secs, want := range map[int64]string{86400: "24h", 604800: "7d", 7776000: "90d", 3600: "1h0m0s"} {
		if got := formatTimer(secs); got != want {
			t.Errorf("formatTimer(%d) = %q, want %q", secs, got, want)
		}
	}
	if got := formatTimer(30 * 86400); !strings.HasPrefix(got, "720h") {
		t.Errorf("formatTimer(30d) = %q", got)
	}
}
