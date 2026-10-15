// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

func groupStore(t *testing.T) *store.Store {
	t.Helper()
	st := storetest.New(t)
	const grp = "120363000000000000@g.us"
	msg := func(id, sender, text string, ts int64) store.Message {
		return store.Message{ChatJID: grp, MessageID: id, SenderJID: sender, Timestamp: ts, Text: text}
	}
	if err := st.Save(context.Background(), nil, []store.Message{
		msg("m1", "31600000001@s.whatsapp.net", "hello group", 1),
		msg("m2", "31600000001@s.whatsapp.net", "hello again", 2),
		msg("m3", "31600000001@s.whatsapp.net", "and once more hello", 3),
		msg("m4", "31600000009@s.whatsapp.net", "hello from a stranger", 4),
	}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSenderNames(t *testing.T) {
	fake := &fakeWA{names: map[string]string{"31600000001@s.whatsapp.net": "Alice"}}
	cs := connect(t, fake, groupStore(t), Options{})

	var page MessagesOut
	call(t, cs, "list_messages", map[string]any{"chat_jid": "120363000000000000@g.us"}, &page)
	if len(page.Messages) != 4 {
		t.Fatalf("got %d messages, want 4", len(page.Messages))
	}
	for _, m := range page.Messages {
		want := "Alice"
		if m.Sender == "31600000009@s.whatsapp.net" {
			want = "" // unknown: field omitted
		}
		if m.SenderName != want {
			t.Errorf("message %s from %s: sender_name = %q, want %q", m.ID, m.Sender, m.SenderName, want)
		}
	}
	if fake.namesCalls != 1 {
		t.Errorf("Names called %d times for one list_messages, want 1", fake.namesCalls)
	}

	var found MessagesOut
	call(t, cs, "search_messages", map[string]any{"query": "hello"}, &found)
	named := 0
	for _, m := range found.Messages {
		if m.SenderName == "Alice" {
			named++
		}
	}
	if named != 3 {
		t.Errorf("search_messages named %d of Alice's messages, want 3", named)
	}
	if fake.namesCalls != 2 {
		t.Errorf("Names calls after search = %d, want 2", fake.namesCalls)
	}
}

func TestSenderNamesLookupFailureKeepsMessages(t *testing.T) {
	fake := &fakeWA{namesErr: errors.New("contacts store unavailable")}
	cs := connect(t, fake, groupStore(t), Options{})

	var page MessagesOut
	res := call(t, cs, "list_messages", map[string]any{"chat_jid": "120363000000000000@g.us"}, &page)
	if res.IsError {
		t.Fatal("a failed name lookup must not fail the tool")
	}
	if len(page.Messages) != 4 {
		t.Errorf("got %d messages, want 4", len(page.Messages))
	}
	for _, m := range page.Messages {
		if m.SenderName != "" {
			t.Errorf("message %s has sender_name %q despite failed lookup", m.ID, m.SenderName)
		}
	}
}

func TestEditedAndDeletedInOutput(t *testing.T) {
	st := groupStore(t)
	ctx := context.Background()
	if ok, err := st.ApplyEdit(ctx, "120363000000000000@g.us", "m1", "hello group (edited)", "", 1_700_000_000); err != nil || !ok {
		t.Fatalf("ApplyEdit: %v %v", ok, err)
	}
	if ok, err := st.MarkDeleted(ctx, "120363000000000000@g.us", "m2"); err != nil || !ok {
		t.Fatalf("MarkDeleted: %v %v", ok, err)
	}
	cs := connect(t, &fakeWA{}, st, Options{})

	var page MessagesOut
	call(t, cs, "list_messages", map[string]any{"chat_jid": "120363000000000000@g.us"}, &page)
	byID := map[string]Message{}
	for _, m := range page.Messages {
		byID[m.ID] = m
	}
	if _, ok := byID["m2"]; ok {
		t.Error("deleted message m2 is listed")
	}
	if m := byID["m1"]; m.Text != "hello group (edited)" || m.EditedAt != "2023-11-14T22:13:20Z" {
		t.Errorf("edited message = %+v", m)
	}
	if m := byID["m3"]; m.EditedAt != "" {
		t.Errorf("unedited message has edited_at %q", m.EditedAt)
	}
}
