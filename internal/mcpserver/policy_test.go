// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

func toolNames(t *testing.T, opts Options) []string {
	t.Helper()
	cs := connect(t, &fakeWA{}, storetest.New(t), opts)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

func TestReadOnlyHidesWriteTools(t *testing.T) {
	got := toolNames(t, Options{ReadOnly: true})
	want := []string{"connection_status", "list_chats", "list_messages", "recent_messages", "search_contacts", "search_messages"}
	if !slices.Equal(got, want) {
		t.Errorf("read-only tools = %v, want %v", got, want)
	}

	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{ReadOnly: true})
	// The tool doesn't exist, so the call fails at the protocol level.
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "send_message", Arguments: map[string]any{"recipient": "+31600000001", "text": "hi"},
	})
	if err == nil && !res.IsError {
		t.Error("send_message in read-only mode: want error")
	}
	if len(fake.sent) != 0 {
		t.Errorf("read-only mode sent %v", fake.sent)
	}
}

func TestAllowRecipients(t *testing.T) {
	// fakeWA's own JID is 31600000000@s.whatsapp.net.
	tests := []struct {
		name      string
		recipient string
		wantSent  string // empty: must be refused
	}{
		{"listed, as JID", "31600000001@s.whatsapp.net", "31600000001@s.whatsapp.net"},
		{"listed, formatted differently", "+31 6 0000 0001", "31600000001@s.whatsapp.net"},
		{"listed group", "120363000000000000@g.us", "120363000000000000@g.us"},
		{"own number is always allowed", "+31600000000", "31600000000@s.whatsapp.net"},
		{"not listed", "+31600000002", ""},
		{"device JID of unlisted user", "31600000002:5@s.whatsapp.net", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeWA{}
			cs := connect(t, fake, storetest.New(t), Options{
				AllowRecipients: []string{"+31 6 0000 0001", "120363000000000000@g.us"},
			})
			res := call(t, cs, "send_message", map[string]any{"recipient": tt.recipient, "text": "hi"}, nil)
			if tt.wantSent == "" {
				if !res.IsError {
					t.Fatal("want tool error")
				}
				if len(fake.sent) != 0 {
					t.Errorf("refused recipient was still sent to: %v", fake.sent)
				}
				return
			}
			if res.IsError {
				t.Fatalf("unexpected error: %v", res.Content)
			}
			if want := []string{tt.wantSent + ": hi"}; !slices.Equal(fake.sent, want) {
				t.Errorf("sent = %v, want %v", fake.sent, want)
			}
		})
	}
}

func TestAllowRecipientsResolvesLIDs(t *testing.T) {
	lids := map[string]string{"111@lid": "31600000001@s.whatsapp.net"}
	tests := []struct {
		name      string
		allow     string
		recipient string
		wantSent  string
	}{
		{"recipient given as LID of an allowed number", "+31600000001", "111@lid", "31600000001@s.whatsapp.net"},
		{"allowlist entry given as LID", "111@lid", "+31600000001", "31600000001@s.whatsapp.net"},
		{"unmapped LID is refused", "+31600000001", "999@lid", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeWA{lids: lids}
			cs := connect(t, fake, storetest.New(t), Options{AllowRecipients: []string{tt.allow}})
			res := call(t, cs, "send_message", map[string]any{"recipient": tt.recipient, "text": "hi"}, nil)
			if tt.wantSent == "" {
				if !res.IsError || len(fake.sent) != 0 {
					t.Fatalf("want refusal, got error=%v sent=%v", res.IsError, fake.sent)
				}
				return
			}
			if want := []string{tt.wantSent + ": hi"}; res.IsError || !slices.Equal(fake.sent, want) {
				t.Errorf("error=%v sent=%v, want %v", res.IsError, fake.sent, want)
			}
		})
	}
}

func TestChatJIDAcceptsLID(t *testing.T) {
	st := storetest.New(t)
	const pn = "31600000001@s.whatsapp.net"
	if err := st.Save(context.Background(), nil, []store.Message{
		{ChatJID: pn, MessageID: "m1", SenderJID: pn, Timestamp: 1, Text: "archived under the phone number"},
	}); err != nil {
		t.Fatal(err)
	}
	cs := connect(t, &fakeWA{lids: map[string]string{"111@lid": pn}}, st, Options{})

	var page MessagesOut
	call(t, cs, "list_messages", map[string]any{"chat_jid": "111@lid"}, &page)
	if len(page.Messages) != 1 {
		t.Errorf("list_messages by LID = %+v, want the phone-number chat's message", page)
	}
	var found MessagesOut
	call(t, cs, "search_messages", map[string]any{"query": "archived", "chat_jid": "111@lid"}, &found)
	if len(found.Messages) != 1 {
		t.Errorf("search_messages by LID = %+v, want 1 hit", found)
	}
}

func TestInvalidAllowedRecipient(t *testing.T) {
	_, err := New("test", &fakeWA{}, storetest.New(t), Options{AllowRecipients: []string{"alice"}})
	if err == nil || !strings.Contains(err.Error(), "alice") {
		t.Errorf("New with invalid allowlist entry: err = %v, want error naming it", err)
	}
}
