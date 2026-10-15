// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"fmt"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

const (
	ctxChat   = "120363000000000000@g.us"
	ctxSender = "31600000001@s.whatsapp.net"
)

func contextStore(t *testing.T) *store.Store {
	t.Helper()
	st := storetest.New(t)
	var msgs []store.Message
	for i := 1; i <= 20; i++ {
		msgs = append(msgs, store.Message{
			ChatJID: ctxChat, MessageID: fmt.Sprintf("m%02d", i), SenderJID: ctxSender, Timestamp: int64(i), Text: "x",
		})
	}
	if err := st.Save(context.Background(), []store.Chat{{JID: ctxChat, Name: "Family"}}, msgs); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestMessageContextTool(t *testing.T) {
	fake := &fakeWA{names: map[string]string{ctxSender: "Alice"}}
	cs := connect(t, fake, contextStore(t), Options{})

	tests := []struct {
		name       string
		args       map[string]any
		wantFirst  string
		wantLen    int
		wantTarget int
	}{
		{"defaults are 5 and 5", map[string]any{}, "m05", 11, 5},
		{"explicit zero before", map[string]any{"before": 0, "after": 2}, "m10", 3, 0},
		{"near the start", map[string]any{"before": 50, "after": 0}, "m01", 10, 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{"chat_jid": ctxChat, "message_id": "m10"}
			for k, v := range tt.args {
				args[k] = v
			}
			var out MessageContextOut
			call(t, cs, "message_context", args, &out)
			if len(out.Messages) != tt.wantLen || out.TargetIndex != tt.wantTarget || out.Messages[0].ID != tt.wantFirst {
				t.Fatalf("len=%d target=%d first=%s, want %d %d %s", len(out.Messages), out.TargetIndex,
					out.Messages[0].ID, tt.wantLen, tt.wantTarget, tt.wantFirst)
			}
			if got := out.Messages[out.TargetIndex].ID; got != "m10" {
				t.Errorf("messages[target_index] = %s, want m10", got)
			}
			for i := 1; i < len(out.Messages); i++ {
				if out.Messages[i].Timestamp < out.Messages[i-1].Timestamp {
					t.Errorf("not oldest first at %d", i)
				}
			}
			if out.ChatName != "Family" || out.Messages[0].SenderName != "Alice" {
				t.Errorf("chat_name=%q sender_name=%q, want Family / Alice", out.ChatName, out.Messages[0].SenderName)
			}
		})
	}
	if fake.namesCalls != len(tests) {
		t.Errorf("Names calls = %d, want one per tool call (%d)", fake.namesCalls, len(tests))
	}
}

func TestMessageContextToolErrors(t *testing.T) {
	cs := connect(t, &fakeWA{}, contextStore(t), Options{})
	for _, args := range []map[string]any{
		{"message_id": "m10"},
		{"chat_jid": ctxChat},
		{"chat_jid": ctxChat, "message_id": "m10", "before": -1},
		{"chat_jid": ctxChat, "message_id": "m10", "after": 51},
		{"chat_jid": ctxChat, "message_id": "unknown"},
	} {
		if res := call(t, cs, "message_context", args, nil); !res.IsError {
			t.Errorf("message_context(%v): want tool error", args)
		}
	}
}
