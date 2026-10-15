// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

func callStore(t *testing.T) *store.Store {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	const grp = "120363000000000000@g.us"
	if err := st.Save(ctx, []store.Chat{{JID: grp, Name: "Family"}}, nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range []store.Call{
		{CallID: "RING", CallerJID: alice, CreatorWire: alice, ChatJID: alice, StartedAt: 300},
		{CallID: "GROUP", CallerJID: alice, CreatorWire: alice, ChatJID: grp, Group: true, Video: true, StartedAt: 200},
		{CallID: "OLD", CallerJID: stranger, CreatorWire: stranger, ChatJID: stranger, StartedAt: 100},
	} {
		if err := st.RecordCallOffer(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.UpdateCall(ctx, "OLD", store.CallEventTerminated, time.Unix(110, 0), "timeout"); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestListCalls(t *testing.T) {
	fake := &fakeWA{names: map[string]string{alice: "Alice"}}
	cs := connect(t, fake, callStore(t), Options{})
	var out ListCallsOut
	call(t, cs, "list_calls", nil, &out)
	var ids []string
	for _, c := range out.Calls {
		ids = append(ids, c.CallID)
	}
	if !slices.Equal(ids, []string{"RING", "GROUP", "OLD"}) {
		t.Fatalf("calls = %v, want newest first", ids)
	}
	ring, group, old := out.Calls[0], out.Calls[1], out.Calls[2]
	if ring.Status != store.CallRinging || ring.CallerName != "Alice" || ring.Group {
		t.Errorf("ringing call = %+v", ring)
	}
	if !group.Group || !group.Video || group.ChatName != "Family" {
		t.Errorf("group call = %+v", group)
	}
	if old.Status != store.CallMissed || old.EndedAt == "" || old.CallerName != "" {
		t.Errorf("missed call = %+v", old)
	}
	if fake.namesCalls != 1 {
		t.Errorf("Names calls = %d, want 1", fake.namesCalls)
	}

	call(t, cs, "list_calls", map[string]any{"since": time.Unix(150, 0).UTC().Format(time.RFC3339), "limit": 1}, &out)
	if len(out.Calls) != 1 || out.Calls[0].CallID != "RING" {
		t.Errorf("since/limit = %+v", out.Calls)
	}
	if res := call(t, cs, "list_calls", map[string]any{"since": "yesterday"}, nil); !res.IsError {
		t.Error("bad since: want error")
	}
}

func TestRejectCall(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, callStore(t), Options{})
	var out CallOut
	call(t, cs, "reject_call", map[string]any{"call_id": "RING"}, &out)
	if out.Status != store.CallRejected || !slices.Equal(fake.sent, []string{"reject RING"}) {
		t.Errorf("out = %+v, sent = %v", out, fake.sent)
	}
	if res := call(t, cs, "reject_call", map[string]any{"call_id": "NOPE"}, nil); !res.IsError {
		t.Error("unknown call: want error")
	}
	if len(fake.sent) != 1 {
		t.Errorf("unknown call reached WhatsApp: %v", fake.sent)
	}
}

func TestRejectCallGuardrails(t *testing.T) {
	fake := &fakeWA{}
	allowlisted := connect(t, fake, callStore(t), Options{AllowRecipients: []string{alice}})
	if res := call(t, allowlisted, "reject_call", map[string]any{"call_id": "OLD"}, nil); !res.IsError {
		t.Error("caller outside the allowlist: want error")
	}
	if len(fake.sent) != 0 {
		t.Errorf("allowlist bypassed: %v", fake.sent)
	}
	if got := toolNames(t, Options{ReadOnly: true}); slices.Contains(got, "reject_call") || !slices.Contains(got, "list_calls") {
		t.Errorf("read-only tools = %v: want list_calls but not reject_call", got)
	}
}
