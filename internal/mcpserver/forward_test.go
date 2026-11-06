// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"slices"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

func TestForwardMessage(t *testing.T) {
	const alice, bob, carol = "31600000001@s.whatsapp.net", "31600000002@s.whatsapp.net", "31600000003@s.whatsapp.net"
	st := storetest.New(t)
	if err := st.Save(context.Background(), nil, []store.Message{
		{ChatJID: alice, MessageID: "m1", SenderJID: alice, Timestamp: 1_700_000_000, Text: "pass it on", Forwarded: true, ForwardingScore: 4},
	}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeWA{}
	cs := connect(t, fake, st, Options{AllowRecipients: []string{bob}})

	var out Message
	if res := call(t, cs, "forward_message", map[string]any{"from_chat_jid": alice, "message_id": "m1", "recipient": "+31 6 0000 0002"}, &out); res.IsError {
		t.Fatalf("forward_message: %v", res.Content)
	}
	if out.ChatJID != bob || !out.Forwarded || !out.ForwardedManyTimes || out.Text != "pass it on" {
		t.Errorf("forwarded = %+v", out)
	}

	for name, args := range map[string]map[string]any{
		"recipient outside the allowlist": {"from_chat_jid": alice, "message_id": "m1", "recipient": carol},
		"unknown message":                 {"from_chat_jid": alice, "message_id": "nope", "recipient": bob},
		"missing recipient":               {"from_chat_jid": alice, "message_id": "m1"},
	} {
		if res := call(t, cs, "forward_message", args, nil); !res.IsError {
			t.Errorf("%s: forward_message succeeded", name)
		}
	}
	if want := []string{"forward " + alice + "/m1 to " + bob}; !slices.Equal(fake.sent, want) {
		t.Errorf("sent = %v, want %v", fake.sent, want)
	}
}
