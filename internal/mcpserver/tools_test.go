// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

type fakeWA struct {
	sent []string
	// lids maps @lid JIDs to phone-number JIDs for Canonical.
	lids map[string]string
}

func (f *fakeWA) Canonical(_ context.Context, jid types.JID) types.JID {
	jid = jid.ToNonAD()
	if pn, ok := f.lids[jid.String()]; ok {
		out, _ := types.ParseJID(pn)
		return out
	}
	return jid
}

func (f *fakeWA) Status() wa.Status {
	return wa.Status{Connected: true, LoggedIn: true, JID: "31600000000@s.whatsapp.net"}
}

func (f *fakeWA) Contacts(context.Context, string) ([]wa.Contact, error) {
	return []wa.Contact{{JID: "31600000001@s.whatsapp.net", Name: "Alice"}}, nil
}

func (f *fakeWA) SendText(_ context.Context, recipient, text string) (store.Message, error) {
	f.sent = append(f.sent, recipient+": "+text)
	return store.Message{ChatJID: recipient, MessageID: "OUT1", Timestamp: 1_700_000_000, FromMe: true, Text: text}, nil
}

func connect(t *testing.T, whatsapp WhatsApp, st *store.Store, opts Options) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	srv, err := New("test", whatsapp, st, opts)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call invokes a tool and decodes its structured output into out.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if out != nil && !res.IsError {
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s: decode %s: %v", name, raw, err)
		}
	}
	return res
}

func TestToolsAreListed(t *testing.T) {
	cs := connect(t, &fakeWA{}, storetest.New(t), Options{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	want := []string{"connection_status", "list_chats", "list_messages", "search_contacts", "search_messages", "send_message"}
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
}

func TestReadTools(t *testing.T) {
	st := storetest.New(t)
	const alice = "31600000001@s.whatsapp.net"
	if err := st.Save(context.Background(), []store.Chat{{JID: alice, Name: "Alice"}}, []store.Message{
		{ChatJID: alice, MessageID: "m1", SenderJID: alice, Timestamp: 1_700_000_000, Text: "see you at the station"},
		{ChatJID: alice, MessageID: "m2", SenderJID: alice, Timestamp: 1_700_000_100, MediaType: "image"},
	}); err != nil {
		t.Fatal(err)
	}
	cs := connect(t, &fakeWA{}, st, Options{})

	var status wa.Status
	call(t, cs, "connection_status", nil, &status)
	if !status.LoggedIn {
		t.Errorf("status = %+v", status)
	}

	var chats ListChatsOut
	call(t, cs, "list_chats", map[string]any{"query": "ali"}, &chats)
	if len(chats.Chats) != 1 || chats.Chats[0].Name != "Alice" || chats.Chats[0].LastMessageAt != "2023-11-14T22:15:00Z" {
		t.Errorf("list_chats = %+v", chats)
	}

	var page MessagesOut
	call(t, cs, "list_messages", map[string]any{"chat_jid": alice, "before": "2023-11-14T22:14:00Z"}, &page)
	if len(page.Messages) != 1 || page.Messages[0].ID != "m1" {
		t.Errorf("list_messages before = %+v", page)
	}

	var found MessagesOut
	call(t, cs, "search_messages", map[string]any{"query": "station"}, &found)
	if len(found.Messages) != 1 || found.Messages[0].Text != "see you at the station" {
		t.Errorf("search_messages = %+v", found)
	}

	var contacts SearchContactsOut
	call(t, cs, "search_contacts", map[string]any{"query": "ali"}, &contacts)
	if len(contacts.Contacts) != 1 {
		t.Errorf("search_contacts = %+v", contacts)
	}
}

func TestToolErrors(t *testing.T) {
	cs := connect(t, &fakeWA{}, storetest.New(t), Options{})
	tests := []struct {
		tool string
		args map[string]any
	}{
		{"list_messages", map[string]any{}},
		{"list_messages", map[string]any{"chat_jid": "x@s.whatsapp.net", "before": "yesterday"}},
		{"search_messages", map[string]any{"query": " "}},
		{"search_contacts", map[string]any{"query": ""}},
		{"send_message", map[string]any{"recipient": "+31600000001", "text": " "}},
	}
	for _, tt := range tests {
		if res := call(t, cs, tt.tool, tt.args, nil); !res.IsError {
			t.Errorf("%s(%v): want tool error", tt.tool, tt.args)
		}
	}
}

func TestSendMessage(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{})
	var sent Message
	call(t, cs, "send_message", map[string]any{"recipient": "+31600000001", "text": "hi"}, &sent)
	if sent.ID != "OUT1" || !sent.FromMe {
		t.Errorf("send_message = %+v", sent)
	}
	if want := []string{"31600000001@s.whatsapp.net: hi"}; !slices.Equal(fake.sent, want) {
		t.Errorf("sent = %v, want %v", fake.sent, want)
	}
}
