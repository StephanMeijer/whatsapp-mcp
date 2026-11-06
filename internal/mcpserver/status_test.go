// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

func TestListStatusUpdates(t *testing.T) {
	const alice, bob = "31600000001@s.whatsapp.net", "31600000002@s.whatsapp.net"
	now := time.Now().Unix()
	st := storetest.New(t)
	if err := st.Save(context.Background(), nil, []store.Message{
		{ChatJID: store.StatusChat, MessageID: "S1", SenderJID: alice, Timestamp: now - 3600, Text: "beach"},
		{ChatJID: store.StatusChat, MessageID: "S2", SenderJID: bob, Timestamp: now - 1800, MediaType: store.MediaImage},
		{ChatJID: store.StatusChat, MessageID: "S3", SenderJID: alice, Timestamp: now - 600, Text: "home"},
		{ChatJID: store.StatusChat, MessageID: "OLD", SenderJID: alice, Timestamp: now - 3*86400, Text: "last week"},
		{ChatJID: alice, MessageID: "M1", SenderJID: alice, Timestamp: now - 60, Text: "hi"},
	}); err != nil {
		t.Fatal(err)
	}
	cs := connect(t, &fakeWA{names: map[string]string{alice: "Alice"}, lids: map[string]string{"111@lid": alice}}, st, Options{ReadOnly: true})

	var out ListStatusUpdatesOut
	if res := call(t, cs, "list_status_updates", map[string]any{}, &out); res.IsError {
		t.Fatalf("list_status_updates: %s", res.Content)
	}
	if len(out.Authors) != 2 || out.Authors[0].JID != alice || out.Authors[0].Name != "Alice" || out.Authors[1].JID != bob {
		t.Fatalf("authors = %+v, want Alice (most recent) then Bob", out.Authors)
	}
	if u := out.Authors[0].Updates; len(u) != 2 || u[0].ID != "S3" || u[1].ID != "S1" {
		t.Errorf("Alice's updates = %+v, want S3 then S1 (the last 24 hours only)", u)
	}
	if out.Authors[1].Updates[0].MediaType != "image" {
		t.Errorf("Bob's update = %+v, want the image", out.Authors[1].Updates[0])
	}

	// Filtered by person, given by LID, with an older since.
	out = ListStatusUpdatesOut{}
	since := time.Unix(now-7*86400, 0).UTC().Format(time.RFC3339)
	if res := call(t, cs, "list_status_updates", map[string]any{"from": "111@lid", "since": since}, &out); res.IsError {
		t.Fatalf("list_status_updates from: %s", res.Content)
	}
	if len(out.Authors) != 1 || len(out.Authors[0].Updates) != 3 {
		t.Errorf("Alice since a week ago = %+v, want her 3 updates", out.Authors)
	}
	if res := call(t, cs, "list_status_updates", map[string]any{"since": "yesterday"}, nil); !res.IsError {
		t.Error("an invalid since was accepted")
	}

	// Statuses are not a chat.
	var chats ListChatsOut
	call(t, cs, "list_chats", map[string]any{}, &chats)
	for _, c := range chats.Chats {
		if c.JID == store.StatusChat {
			t.Error("list_chats shows the status chat")
		}
	}
}
