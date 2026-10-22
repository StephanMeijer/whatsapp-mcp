// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"

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
	// names are returned by Names; namesErr makes it fail.
	names      map[string]string
	namesErr   error
	namesCalls int

	// lastMedia is the file passed to the latest SendMedia.
	lastMedia []byte

	// media is returned by Media, or mediaErr.
	media    wa.Media
	mediaErr error

	// fetch is returned by FetchOlder; fetchArgs records each call.
	fetch     wa.FetchResult
	fetchErr  error
	fetchArgs []string

	// registered maps the phone numbers that are on WhatsApp to their JIDs
	// for CheckNumbers; checkCalls records the numbers asked about, per call.
	registered map[string]string
	checkErr   error
	checkCalls [][]string

	// groups are returned by GroupInfo, keyed by JID; groupCalls records the
	// JIDs asked about.
	groups     map[string]wa.Group
	groupErr   error
	groupCalls []string

	// contacts are returned by ContactInfo, keyed by JID; picture by
	// ProfilePicture. The call recorders note the JIDs asked about.
	contacts     map[string]wa.ContactInfo
	contactErr   error
	contactCalls []string
	picture      wa.Media
	pictureErr   error
	pictureCalls []string
}

func (f *fakeWA) ContactInfo(_ context.Context, jid types.JID) (wa.ContactInfo, error) {
	f.contactCalls = append(f.contactCalls, jid.String())
	if f.contactErr != nil {
		return wa.ContactInfo{}, f.contactErr
	}
	c, ok := f.contacts[jid.String()]
	if !ok {
		return wa.ContactInfo{}, fmt.Errorf("WhatsApp has no info for %s", jid)
	}
	return c, nil
}

func (f *fakeWA) ProfilePicture(_ context.Context, jid types.JID, preview bool) (wa.Media, error) {
	f.pictureCalls = append(f.pictureCalls, fmt.Sprintf("%s preview=%v", jid, preview))
	return f.picture, f.pictureErr
}

func (f *fakeWA) GroupInfo(_ context.Context, group types.JID) (wa.Group, error) {
	f.groupCalls = append(f.groupCalls, group.String())
	if f.groupErr != nil {
		return wa.Group{}, f.groupErr
	}
	g, ok := f.groups[group.String()]
	if !ok {
		return wa.Group{}, errors.New("group info: that group does not exist")
	}
	return g, nil
}

func (f *fakeWA) CheckNumbers(_ context.Context, phones []string) ([]wa.NumberCheck, error) {
	f.checkCalls = append(f.checkCalls, phones)
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	out := make([]wa.NumberCheck, len(phones))
	for i, p := range phones {
		jid, ok := f.registered[p]
		out[i] = wa.NumberCheck{Phone: p, OnWhatsApp: ok, JID: jid}
	}
	return out, nil
}

func (f *fakeWA) FetchOlder(_ context.Context, chatJID string, count int, wait time.Duration) (wa.FetchResult, error) {
	f.fetchArgs = append(f.fetchArgs, fmt.Sprintf("%s count=%d wait=%s", chatJID, count, wait))
	return f.fetch, f.fetchErr
}

func (f *fakeWA) Names(_ context.Context, jids []types.JID) (map[types.JID]string, error) {
	f.namesCalls++
	if f.namesErr != nil {
		return nil, f.namesErr
	}
	out := map[types.JID]string{}
	for _, j := range jids {
		if n, ok := f.names[j.String()]; ok {
			out[j] = n
		}
	}
	return out, nil
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

func (f *fakeWA) SendText(_ context.Context, recipient, text string, quoted *store.Message) (store.Message, error) {
	entry := recipient + ": " + text
	if quoted != nil {
		entry += " (reply to " + quoted.MessageID + ")"
	}
	f.sent = append(f.sent, entry)
	return store.Message{ChatJID: recipient, MessageID: "OUT1", Timestamp: 1_700_000_000, FromMe: true, Text: text}, nil
}

func (f *fakeWA) React(_ context.Context, chat types.JID, target store.Message, emoji string) error {
	f.sent = append(f.sent, fmt.Sprintf("react %s/%s %q", chat, target.MessageID, emoji))
	return nil
}

func (f *fakeWA) EditMessage(_ context.Context, chat types.JID, target store.Message, text string) (store.Message, error) {
	f.sent = append(f.sent, fmt.Sprintf("edit %s/%s %q", chat, target.MessageID, text))
	target.Text, target.EditedAt = text, 1_700_000_000
	return target, nil
}

func (f *fakeWA) DeleteMessage(_ context.Context, chat types.JID, target store.Message) error {
	f.sent = append(f.sent, fmt.Sprintf("delete %s/%s", chat, target.MessageID))
	return nil
}

func (f *fakeWA) Media(_ context.Context, m store.Message) (wa.Media, error) {
	if f.mediaErr != nil {
		return wa.Media{}, f.mediaErr
	}
	return wa.Media{Kind: m.MediaType, MIMEType: f.media.MIMEType, FileName: f.media.FileName, Data: f.media.Data}, nil
}

func (f *fakeWA) SendMedia(_ context.Context, recipient string, m wa.OutgoingMedia) (store.Message, error) {
	f.sent = append(f.sent, fmt.Sprintf("media %s %s %q %d bytes %q", recipient, m.Kind, m.FileName, len(m.Data), m.Caption))
	f.lastMedia = m.Data
	return store.Message{ChatJID: recipient, MessageID: "MEDIA1", Timestamp: 1_700_000_000, FromMe: true, MediaType: m.Kind, Text: m.Caption}, nil
}

func (f *fakeWA) RejectCall(_ context.Context, callID string) (store.Call, error) {
	f.sent = append(f.sent, "reject "+callID)
	return store.Call{CallID: callID, CallerJID: "31600000001@s.whatsapp.net", Status: store.CallRejected, StartedAt: 1}, nil
}

func (f *fakeWA) SetChatState(_ context.Context, chat types.JID, fl store.ChatFlags) error {
	show := func(v any) string {
		switch v := v.(type) {
		case *bool:
			if v != nil {
				return fmt.Sprint(*v)
			}
		case *int64:
			if v != nil {
				return fmt.Sprint(*v)
			}
		}
		return "-"
	}
	f.sent = append(f.sent, fmt.Sprintf("state %s archived=%s pinned=%s muted=%s", chat, show(fl.Archived), show(fl.Pinned), show(fl.MutedUntil)))
	return nil
}

func (f *fakeWA) MarkChatRead(_ context.Context, chat types.JID) error {
	f.sent = append(f.sent, "read "+chat.String())
	return nil
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
	want := []string{"check_number", "connection_status", "contact_info", "delete_message", "edit_message", "fetch_older_messages", "get_media", "get_profile_picture", "group_info", "list_calls", "list_chats", "list_messages", "mark_read", "message_context", "message_receipts", "react", "recent_messages", "reject_call", "search_contacts", "search_messages", "send_media", "send_message", "set_chat_state"}
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
