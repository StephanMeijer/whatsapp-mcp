// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"slices"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

func TestPresenceToolsNeedTheFlag(t *testing.T) {
	has := func(opts Options) (out []string) {
		for _, n := range toolNames(t, opts) {
			if n == "get_presence" || n == "set_typing" {
				out = append(out, n)
			}
		}
		return out
	}
	if got := has(Options{}); len(got) != 0 {
		t.Errorf("without the flag: %v offered", got)
	}
	if got := has(Options{Presence: true}); !slices.Equal(got, []string{"get_presence", "set_typing"}) {
		t.Errorf("with the flag: %v offered", got)
	}
	if got := has(Options{Presence: true, ReadOnly: true}); !slices.Equal(got, []string{"get_presence"}) {
		t.Errorf("read-only with the flag: %v offered, want only get_presence", got)
	}
}

func TestGetPresence(t *testing.T) {
	const group = "120363000000000001@g.us"
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{Presence: true, AllowRecipients: []string{alice, group}})
	var out wa.Presence
	if res := call(t, cs, "get_presence", map[string]any{"jid": "+31 6 0000 0001"}, &out); res.IsError {
		t.Fatalf("get_presence: %v", res.Content)
	}
	if want := (wa.Presence{JID: alice, State: "online"}); out != want {
		t.Errorf("get_presence = %+v, want %+v", out, want)
	}
	for name, jid := range map[string]any{"missing": nil, "garbage": "not a jid", "group": group, "not on the allowlist": "31600000009@s.whatsapp.net"} {
		if res := call(t, cs, "get_presence", map[string]any{"jid": jid}, nil); !res.IsError {
			t.Errorf("get_presence, jid %s: want tool error", name)
		}
	}
	if want := []string{"presence " + alice}; !slices.Equal(fake.sent, want) {
		t.Errorf("WhatsApp got %v, want %v", fake.sent, want)
	}
}

func TestSetTyping(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{Presence: true, AllowRecipients: []string{alice}})
	for _, state := range []string{"typing", "recording", "paused"} {
		var out SetTypingOut
		if res := call(t, cs, "set_typing", map[string]any{"chat_jid": alice, "state": state}, &out); res.IsError {
			t.Fatalf("set_typing %s: %v", state, res.Content)
		}
		if want := (SetTypingOut{ChatJID: alice, State: state}); out != want {
			t.Errorf("set_typing %s = %+v", state, out)
		}
	}
	for name, args := range map[string]map[string]any{
		"no chat":              {"state": "typing"},
		"bad state":            {"chat_jid": alice, "state": "dancing"},
		"not on the allowlist": {"chat_jid": "31600000009@s.whatsapp.net", "state": "typing"},
	} {
		if res := call(t, cs, "set_typing", args, nil); !res.IsError {
			t.Errorf("set_typing, %s: want tool error", name)
		}
	}
	want := []string{"typing " + alice + " composing ", "typing " + alice + " composing audio", "typing " + alice + " paused "}
	if !slices.Equal(fake.sent, want) {
		t.Errorf("WhatsApp got %q, want %q", fake.sent, want)
	}
}
