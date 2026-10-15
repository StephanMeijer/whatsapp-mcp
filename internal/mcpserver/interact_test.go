// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"slices"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

const (
	alice    = "31600000001@s.whatsapp.net"
	stranger = "31600000009@s.whatsapp.net"
)

func interactStore(t *testing.T) *store.Store {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	if err := st.Save(ctx, nil, []store.Message{
		{ChatJID: alice, MessageID: "A1", SenderJID: alice, Timestamp: 1, Text: "dinner at 8?"},
		{ChatJID: alice, MessageID: "MINE", SenderJID: "31600000000@s.whatsapp.net", FromMe: true, Timestamp: 4, Text: "oops"},
		{ChatJID: alice, MessageID: "GONE", SenderJID: alice, Timestamp: 2, Text: "deleted"},
		{ChatJID: alice, MessageID: "MINEGONE", SenderJID: "31600000000@s.whatsapp.net", FromMe: true, Timestamp: 6, Text: "already deleted"},
		{ChatJID: stranger, MessageID: "S1", SenderJID: stranger, Timestamp: 3, Text: "hi"},
		{ChatJID: stranger, MessageID: "S2", SenderJID: "31600000000@s.whatsapp.net", FromMe: true, Timestamp: 5, Text: "hello"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkDeleted(ctx, alice, "GONE"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkDeleted(ctx, alice, "MINEGONE"); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestReplyReactMarkRead(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, interactStore(t), Options{})

	call(t, cs, "send_message", map[string]any{"recipient": alice, "text": "sounds good", "reply_to": "A1"}, nil)
	var reacted ReactOut
	call(t, cs, "react", map[string]any{"chat_jid": alice, "message_id": "A1", "emoji": "👍"}, &reacted)
	call(t, cs, "react", map[string]any{"chat_jid": alice, "message_id": "A1", "emoji": ""}, &reacted)
	if !reacted.Removed {
		t.Error("empty emoji: want removed")
	}
	call(t, cs, "mark_read", map[string]any{"chat_jid": "+31 6 0000 0001"}, nil)
	var edited Message
	call(t, cs, "edit_message", map[string]any{"chat_jid": alice, "message_id": "MINE", "text": "oops, fixed"}, &edited)
	if edited.Text != "oops, fixed" || edited.EditedAt == "" || !edited.FromMe {
		t.Errorf("edit_message = %+v", edited)
	}
	var deleted DeleteMessageOut
	call(t, cs, "delete_message", map[string]any{"chat_jid": alice, "message_id": "MINE"}, &deleted)
	if !deleted.Deleted || deleted.MessageID != "MINE" {
		t.Errorf("delete_message = %+v", deleted)
	}

	want := []string{
		alice + ": sounds good (reply to A1)",
		`react ` + alice + `/A1 "👍"`,
		`react ` + alice + `/A1 ""`,
		"read " + alice,
		"edit " + alice + `/MINE "oops, fixed"`,
		"delete " + alice + "/MINE",
	}
	if !slices.Equal(fake.sent, want) {
		t.Errorf("calls =\n%q\nwant\n%q", fake.sent, want)
	}
}

func TestInteractErrorsSendNothing(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, interactStore(t), Options{})
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"send_message", map[string]any{"recipient": alice, "text": "x", "reply_to": "UNKNOWN"}},
		{"send_message", map[string]any{"recipient": alice, "text": "x", "reply_to": "GONE"}},
		{"send_message", map[string]any{"recipient": alice, "text": "x", "reply_to": "S1"}}, // other chat
		{"react", map[string]any{"chat_jid": alice, "message_id": "UNKNOWN", "emoji": "👍"}},
		{"react", map[string]any{"chat_jid": alice, "message_id": "A1", "emoji": "not an emoji"}},
		{"react", map[string]any{"chat_jid": alice, "emoji": "👍"}},
		{"mark_read", map[string]any{}},
		{"edit_message", map[string]any{"chat_jid": alice, "message_id": "A1", "text": "x"}}, // not yours
		{"edit_message", map[string]any{"chat_jid": alice, "message_id": "UNKNOWN", "text": "x"}},
		{"edit_message", map[string]any{"chat_jid": alice, "message_id": "GONE", "text": "x"}},
		{"edit_message", map[string]any{"chat_jid": alice, "message_id": "S2", "text": "x"}}, // other chat
		{"edit_message", map[string]any{"chat_jid": alice, "message_id": "MINE", "text": " "}},
		{"delete_message", map[string]any{"chat_jid": alice, "message_id": "A1"}}, // not yours
		{"delete_message", map[string]any{"chat_jid": alice, "message_id": "UNKNOWN"}},
		{"delete_message", map[string]any{"chat_jid": alice, "message_id": "GONE"}},
		{"delete_message", map[string]any{"chat_jid": alice, "message_id": "MINEGONE"}}, // yours, already deleted
		{"delete_message", map[string]any{"chat_jid": alice, "message_id": "S1"}},       // other chat
		{"delete_message", map[string]any{"chat_jid": alice}},
	} {
		if res := call(t, cs, c.tool, c.args, nil); !res.IsError {
			t.Errorf("%s(%v): want tool error", c.tool, c.args)
		}
	}
	if len(fake.sent) != 0 {
		t.Errorf("failed calls still reached WhatsApp: %v", fake.sent)
	}
}

func TestInteractRespectsAllowlist(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, interactStore(t), Options{AllowRecipients: []string{alice}})
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"send_message", map[string]any{"recipient": stranger, "text": "x", "reply_to": "S1"}},
		{"react", map[string]any{"chat_jid": stranger, "message_id": "S1", "emoji": "👍"}},
		{"mark_read", map[string]any{"chat_jid": stranger}},
		{"edit_message", map[string]any{"chat_jid": stranger, "message_id": "S2", "text": "x"}},
		{"delete_message", map[string]any{"chat_jid": stranger, "message_id": "S2"}},
	} {
		if res := call(t, cs, c.tool, c.args, nil); !res.IsError {
			t.Errorf("%s to a chat outside the allowlist: want tool error", c.tool)
		}
	}
	if len(fake.sent) != 0 {
		t.Errorf("allowlist bypassed: %v", fake.sent)
	}
	// The allowed chat still works.
	call(t, cs, "react", map[string]any{"chat_jid": alice, "message_id": "A1", "emoji": "👍"}, nil)
	if len(fake.sent) != 1 {
		t.Errorf("allowed react not sent: %v", fake.sent)
	}
}
