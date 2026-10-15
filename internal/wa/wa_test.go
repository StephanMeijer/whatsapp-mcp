// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestMessageContent(t *testing.T) {
	tests := []struct {
		name      string
		msg       *waE2E.Message
		text, typ string
	}{
		{"conversation", &waE2E.Message{Conversation: proto.String("hi")}, "hi", ""},
		{"extended text", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("link https://x")}}, "link https://x", ""},
		{"image with caption", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("beach")}}, "beach", "image"},
		{"audio", &waE2E.Message{AudioMessage: &waE2E.AudioMessage{}}, "", "audio"},
		{"document falls back to file name", &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("invoice.pdf")}}, "invoice.pdf", "document"},
		{"reaction is ignored", &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("👍")}}, "", ""},
		{"nil", nil, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, typ := messageContent(tt.msg)
			if text != tt.text || typ != tt.typ {
				t.Errorf("messageContent = %q, %q; want %q, %q", text, typ, tt.text, tt.typ)
			}
		})
	}
}

func TestToMessage(t *testing.T) {
	ts := time.Unix(1_700_000_000, 0)
	e := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("31600000001", types.DefaultUserServer),
				Sender: types.NewADJID("31600000001", 0, 3),
			},
			ID:        "ABC",
			Timestamp: ts,
		},
		Message: &waE2E.Message{Conversation: proto.String("hello")},
	}
	m, ok := toMessage(e, types.JID.ToNonAD)
	if !ok {
		t.Fatal("toMessage: not ok")
	}
	if m.ChatJID != "31600000001@s.whatsapp.net" || m.SenderJID != "31600000001@s.whatsapp.net" ||
		m.MessageID != "ABC" || m.Timestamp != ts.Unix() || m.Text != "hello" {
		t.Errorf("toMessage = %+v", m)
	}

	e.IsEdit = true
	if _, ok := toMessage(e, types.JID.ToNonAD); ok {
		t.Error("edits should be skipped")
	}
}

func TestToMessageCanonicalisesLIDs(t *testing.T) {
	lid := types.NewJID("111", types.HiddenUserServer)
	pn := types.NewJID("31600000001", types.DefaultUserServer)
	canon := func(j types.JID) types.JID {
		if j.ToNonAD() == lid {
			return pn
		}
		return j.ToNonAD()
	}
	e := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: lid, Sender: types.NewADJID("111", 0, 2)},
			ID:            "X",
		},
		Message: &waE2E.Message{Conversation: proto.String("hi")},
	}
	// NewADJID builds a phone-number JID; give it the LID server like a real LID device.
	e.Info.Sender.Server = types.HiddenUserServer
	m, ok := toMessage(e, canon)
	if !ok {
		t.Fatal("toMessage: not ok")
	}
	if m.ChatJID != pn.String() || m.SenderJID != pn.String() {
		t.Errorf("chat %s, sender %s; want both %s", m.ChatJID, m.SenderJID, pn)
	}
}

func TestMergeContacts(t *testing.T) {
	lid := types.NewJID("111", types.HiddenUserServer)
	pn := types.NewJID("31600000001", types.DefaultUserServer)
	other := types.NewJID("31600000002", types.DefaultUserServer)
	unmapped := types.NewJID("999", types.HiddenUserServer)
	canon := func(j types.JID) types.JID {
		if j == lid {
			return pn
		}
		return j
	}
	got := mergeContacts(map[types.JID]types.ContactInfo{
		pn:       {Found: true, FullName: "Alice Saved"},
		lid:      {FullName: "ignored, PN entry wins", PushName: "alice"},
		other:    {PushName: "Bob"},
		unmapped: {PushName: "Stranger"},
	}, canon)

	want := map[types.JID]types.ContactInfo{
		pn:       {Found: true, FullName: "Alice Saved", PushName: "alice"},
		other:    {PushName: "Bob"},
		unmapped: {PushName: "Stranger"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d contacts, want %d: %+v", len(got), len(want), got)
	}
	for jid, w := range want {
		if got[jid] != w {
			t.Errorf("%s = %+v, want %+v", jid, got[jid], w)
		}
	}
}

func TestParseRecipient(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{in: "+31 6 1234-5678", want: "31612345678@s.whatsapp.net"},
		{in: "31612345678@s.whatsapp.net", want: "31612345678@s.whatsapp.net"},
		{in: "120363000000000000@g.us", want: "120363000000000000@g.us"},
		{in: "alice", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseRecipient(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseRecipient(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil || got.String() != tt.want {
				t.Errorf("ParseRecipient(%q) = %v, %v; want %s", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestResolveNames(t *testing.T) {
	saved := types.NewJID("31600000001", types.DefaultUserServer)
	pushOnly := types.NewJID("31600000002", types.DefaultUserServer)
	viaLID := types.NewJID("31600000003", types.DefaultUserServer)
	lid := types.NewJID("333", types.HiddenUserServer)
	unknown := types.NewJID("31600000004", types.DefaultUserServer)
	self := types.NewJID("31600000000", types.DefaultUserServer)
	group := types.NewJID("120363000000000000", types.GroupServer)

	all := map[types.JID]types.ContactInfo{
		saved:    {FullName: "Alice Saved", PushName: "alice"},
		pushOnly: {PushName: "Bob"},
		lid:      {PushName: "Carol via LID"},
	}
	lidCalls := 0
	lidFor := func(pn types.JID) types.JID {
		lidCalls++
		if pn == viaLID {
			return lid
		}
		return types.JID{}
	}

	got := resolveNames(all, []types.JID{saved, pushOnly, viaLID, unknown, self, saved, group}, self, "Me Myself", lidFor)
	want := map[types.JID]string{
		saved:    "Alice Saved",
		pushOnly: "Bob",
		viaLID:   "Carol via LID",
		self:     "Me Myself",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for jid, name := range want {
		if got[jid] != name {
			t.Errorf("%s = %q, want %q", jid, got[jid], name)
		}
	}
	if _, ok := got[unknown]; ok {
		t.Error("unknown sender must be absent, not an empty name")
	}
	// Only the phone numbers without a name of their own need the LID lookup.
	if lidCalls != 2 {
		t.Errorf("LID lookups = %d, want 2 (viaLID and unknown)", lidCalls)
	}
}
