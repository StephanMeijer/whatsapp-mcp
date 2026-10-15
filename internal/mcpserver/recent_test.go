// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"slices"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

func TestRecentMessages(t *testing.T) {
	st := storetest.New(t)
	const (
		alice = "31600000001@s.whatsapp.net"
		grp   = "120363000000000000@g.us"
		day   = 1_760_486_400 // 2025-10-15T00:00:00Z
	)
	m := func(chat, id string, offset int64, fromMe bool) store.Message {
		return store.Message{ChatJID: chat, MessageID: id, SenderJID: alice, Timestamp: day + offset, FromMe: fromMe, Text: id}
	}
	if err := st.Save(context.Background(),
		[]store.Chat{{JID: alice, Name: "Alice"}, {JID: grp, Name: "Family"}},
		[]store.Message{
			m(alice, "yesterday", -60, false),
			m(alice, "a1", 10, false),
			m(grp, "g1", 20, false),
			m(alice, "a2", 30, false),
			m(alice, "mine", 35, true),
			m(grp, "g2", 40, false), // latest: the group comes first
		}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeWA{names: map[string]string{alice: "Alice"}}
	cs := connect(t, fake, st, Options{})

	var out RecentMessagesOut
	call(t, cs, "recent_messages", map[string]any{"since": "2025-10-15T00:00:00Z"}, &out)

	type chatIDs struct {
		name string
		ids  []string
	}
	var got []chatIDs
	for _, c := range out.Chats {
		var ids []string
		for _, msg := range c.Messages {
			ids = append(ids, msg.ID)
			if msg.SenderName != "Alice" {
				t.Errorf("message %s: sender_name = %q, want Alice", msg.ID, msg.SenderName)
			}
		}
		got = append(got, chatIDs{c.Name, ids})
	}
	want := []chatIDs{{"Family", []string{"g1", "g2"}}, {"Alice", []string{"a1", "a2"}}}
	if len(got) != len(want) {
		t.Fatalf("chats = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].name != want[i].name || !slices.Equal(got[i].ids, want[i].ids) {
			t.Errorf("chat %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if out.Total != 4 || out.Truncated {
		t.Errorf("total=%d truncated=%v, want 4 false", out.Total, out.Truncated)
	}
	if fake.namesCalls != 1 {
		t.Errorf("Names calls = %d, want 1", fake.namesCalls)
	}

	call(t, cs, "recent_messages", map[string]any{"since": "2025-10-15T00:00:00Z", "limit": 2, "include_from_me": true}, &out)
	if out.Total != 2 || !out.Truncated {
		t.Errorf("limited: total=%d truncated=%v, want 2 true", out.Total, out.Truncated)
	}
	if len(out.Chats) != 2 || out.Chats[0].Messages[0].ID != "g2" || out.Chats[1].Messages[0].ID != "mine" {
		t.Errorf("limited result should be the newest two (g2, mine): %+v", out.Chats)
	}
}

func TestRecentMessagesErrors(t *testing.T) {
	cs := connect(t, &fakeWA{}, storetest.New(t), Options{})
	for _, args := range []map[string]any{
		{},
		{"since": "today"},
		{"since": "2025-10-15T00:00:00Z", "until": "2025-10-14T00:00:00Z"},
		{"since": "2025-10-15T00:00:00Z", "until": "2025-10-15T00:00:00Z"},
		{"since": "2025-10-15T00:00:00Z", "until": "tomorrow"},
	} {
		if res := call(t, cs, "recent_messages", args, nil); !res.IsError {
			t.Errorf("recent_messages(%v): want tool error", args)
		}
	}

	var empty RecentMessagesOut
	call(t, cs, "recent_messages", map[string]any{"since": "2025-10-15T00:00:00Z"}, &empty)
	if empty.Chats == nil || len(empty.Chats) != 0 || empty.Total != 0 {
		t.Errorf("no messages: got %+v, want an empty chats list", empty)
	}
}
