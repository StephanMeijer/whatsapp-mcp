// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

const (
	me    = "31600000000@s.whatsapp.net"
	bob   = "31600000002@s.whatsapp.net"
	group = "123@g.us"
)

// receiptStore has an own message to alice that was read, alice's reply,
// and an own group message bob delivered and alice read.
func receiptStore(t *testing.T) *store.Store {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	if err := st.Save(ctx, nil, []store.Message{
		{ChatJID: alice, MessageID: "own", SenderJID: me, Timestamp: 10, FromMe: true, Text: "hi"},
		{ChatJID: alice, MessageID: "theirs", SenderJID: alice, Timestamp: 11, Text: "hello"},
		{ChatJID: group, MessageID: "g1", SenderJID: me, Timestamp: 12, FromMe: true, Text: "all"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		chat, id, who, status string
		sec                   int64
	}{
		{alice, "own", alice, store.StatusDelivered, 100},
		{alice, "own", alice, store.StatusRead, 200},
		{group, "g1", bob, store.StatusDelivered, 300},
		{group, "g1", alice, store.StatusDelivered, 310},
		{group, "g1", alice, store.StatusRead, 320},
	} {
		if _, err := st.RecordReceipt(ctx, r.chat, r.id, r.who, r.status, time.Unix(r.sec, 0)); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func TestOwnMessagesCarryStatus(t *testing.T) {
	cs := connect(t, &fakeWA{}, receiptStore(t), Options{})
	var out MessagesOut
	if res := call(t, cs, "list_messages", map[string]any{"chat_jid": alice}, &out); res.IsError {
		t.Fatalf("list_messages: %v", res.Content)
	}
	got := map[string]string{}
	for _, m := range out.Messages {
		got[m.ID] = m.Status
	}
	if want := map[string]string{"own": "read", "theirs": ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("statuses = %v, want %v", got, want)
	}
}

func TestMessageReceipts(t *testing.T) {
	fake := &fakeWA{names: map[string]string{alice: "Alice"}}
	cs := connect(t, fake, receiptStore(t), Options{ReadOnly: true})
	var out MessageReceiptsOut
	if res := call(t, cs, "message_receipts", map[string]any{"chat_jid": group, "message_id": "g1"}, &out); res.IsError {
		t.Fatalf("message_receipts: %v", res.Content)
	}
	want := MessageReceiptsOut{Status: "read", Receipts: []ReceiptOut{
		{JID: alice, Name: "Alice", DeliveredAt: "1970-01-01T00:05:10Z", ReadAt: "1970-01-01T00:05:20Z"},
		{JID: bob, DeliveredAt: "1970-01-01T00:05:00Z"},
	}}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("message_receipts =\n %+v\nwant\n %+v", out, want)
	}
}

func TestMessageReceiptsErrors(t *testing.T) {
	cs := connect(t, &fakeWA{}, receiptStore(t), Options{})
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing args", map[string]any{"chat_jid": alice}, "required"},
		{"incoming message", map[string]any{"chat_jid": alice, "message_id": "theirs"}, "only messages you sent"},
		{"unknown message", map[string]any{"chat_jid": alice, "message_id": "nope"}, "not found"},
	}
	for _, tt := range tests {
		res := call(t, cs, "message_receipts", tt.args, nil)
		if got := res.Content[0].(*mcp.TextContent).Text; !res.IsError || !strings.Contains(got, tt.want) {
			t.Errorf("%s: error = %v %q, want %q", tt.name, res.IsError, got, tt.want)
		}
	}
}
