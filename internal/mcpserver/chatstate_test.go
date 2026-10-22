// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

// chatStateStore has alice (plain, most recent), bob (pinned) and carol (archived).
func chatStateStore(t *testing.T) *store.Store {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	if err := st.Save(ctx, []store.Chat{
		{JID: alice, Name: "Alice", LastMessageAt: 30},
		{JID: bob, Name: "Bob", LastMessageAt: 20},
		{JID: carol, Name: "Carol", LastMessageAt: 10},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.SetChatFlags(ctx, bob, store.ChatFlags{Pinned: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetChatFlags(ctx, carol, store.ChatFlags{Archived: ptr(true), MutedUntil: ptr(int64(-1))}); err != nil {
		t.Fatal(err)
	}
	return st
}

const carol = "31600000003@s.whatsapp.net"

func TestListChatsArchivedAndPinned(t *testing.T) {
	cs := connect(t, &fakeWA{}, chatStateStore(t), Options{ReadOnly: true})
	tests := []struct {
		archived string
		want     []string
	}{
		{"", []string{bob, alice}},
		{"exclude", []string{bob, alice}},
		{"include", []string{bob, alice, carol}},
		{"only", []string{carol}},
	}
	for _, tt := range tests {
		var out ListChatsOut
		if res := call(t, cs, "list_chats", map[string]any{"archived": tt.archived}, &out); res.IsError {
			t.Fatalf("list_chats(archived=%q): %v", tt.archived, res.Content)
		}
		var got []string
		for _, c := range out.Chats {
			got = append(got, c.JID)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("list_chats(archived=%q) = %v, want %v", tt.archived, got, tt.want)
		}
		for _, c := range out.Chats {
			switch c.JID {
			case bob:
				if !c.Pinned || c.Archived || c.MutedUntil != "" {
					t.Errorf("bob = %+v", c)
				}
			case carol:
				if !c.Archived || c.MutedUntil != "forever" {
					t.Errorf("carol = %+v", c)
				}
			}
		}
	}
	if res := call(t, cs, "list_chats", map[string]any{"archived": "yes"}, nil); !res.IsError {
		t.Error("list_chats(archived=yes): want tool error")
	}
}

func TestSetChatState(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, chatStateStore(t), Options{})

	// A fresh output per call: omitted fields would otherwise keep old values.
	set := func(args map[string]any) Chat {
		t.Helper()
		var out Chat
		if res := call(t, cs, "set_chat_state", args, &out); res.IsError {
			t.Fatalf("set_chat_state(%v): %v", args, res.Content)
		}
		return out
	}
	if out := set(map[string]any{"chat_jid": "+31 6 0000 0001", "archived": true}); !out.Archived || out.JID != alice || out.Name != "Alice" {
		t.Errorf("archived alice = %+v", out)
	}
	if out := set(map[string]any{"chat_jid": alice, "archived": false, "pinned": true, "mute": "forever"}); out.Archived || !out.Pinned || out.MutedUntil != "forever" {
		t.Errorf("alice = %+v", out)
	}
	out := set(map[string]any{"chat_jid": alice, "mute": "8h"})
	until, err := time.Parse(time.RFC3339, out.MutedUntil)
	if err != nil || until.Sub(time.Now().Add(8*time.Hour)).Abs() > time.Minute {
		t.Errorf("muted_until = %q, want about 8h from now", out.MutedUntil)
	}
	if out := set(map[string]any{"chat_jid": alice, "mute": "off"}); out.MutedUntil != "" || !out.Pinned {
		t.Errorf("unmuted alice = %+v", out)
	}

	want := []string{
		"state " + alice + " archived=true pinned=- muted=-",
		"state " + alice + " archived=false pinned=true muted=-1",
		"state " + alice + " archived=- pinned=- muted=" + strconv.FormatInt(until.Unix(), 10),
		"state " + alice + " archived=- pinned=- muted=0",
	}
	if !slices.Equal(fake.sent, want) {
		t.Errorf("sent = %v, want %v", fake.sent, want)
	}
	// Archived chats stay hidden from the default listing after changes.
	var chats ListChatsOut
	call(t, cs, "list_chats", map[string]any{}, &chats)
	if len(chats.Chats) == 0 || chats.Chats[0].JID != alice || !chats.Chats[0].Pinned {
		t.Errorf("list_chats after pin = %+v, want alice first and pinned", chats.Chats)
	}
}
