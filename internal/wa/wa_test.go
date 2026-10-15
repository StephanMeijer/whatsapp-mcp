// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"

	"go.mau.fi/whatsmeow/proto/waCommon"
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

func TestToChange(t *testing.T) {
	chat := types.NewJID("31600000001", types.DefaultUserServer)
	at := time.Unix(1_700_000_000, 0)
	event := func(m *waE2E.Message, isEdit bool) *events.Message {
		return &events.Message{
			Info: types.MessageInfo{
				MessageSource: types.MessageSource{Chat: chat, Sender: chat},
				ID:            "WRAPPER", Timestamp: at,
			},
			Message: m,
			IsEdit:  isEdit,
		}
	}
	key := &waCommon.MessageKey{ID: proto.String("ORIGINAL")}
	editOf := func(content *waE2E.Message, tsMS int64) *waE2E.Message {
		pm := &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: key, EditedMessage: content}
		if tsMS != 0 {
			pm.TimestampMS = proto.Int64(tsMS)
		}
		return &waE2E.Message{ProtocolMessage: pm}
	}

	tests := []struct {
		name string
		e    *events.Message
		want change
		ok   bool
	}{
		{
			name: "text edit",
			e:    event(editOf(&waE2E.Message{Conversation: proto.String("fixed typo")}, 1_700_000_100_000), true),
			want: change{kind: changeEdit, chat: chat.String(), target: "ORIGINAL", text: "fixed typo", at: 1_700_000_100},
			ok:   true,
		},
		{
			name: "caption edit without its own timestamp",
			e:    event(editOf(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("new caption")}}, 0), true),
			want: change{kind: changeEdit, chat: chat.String(), target: "ORIGINAL", text: "new caption", media: "image", at: at.Unix()},
			ok:   true,
		},
		{
			name: "deleted for everyone",
			e: event(&waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
				Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: key,
			}}, false),
			want: change{kind: changeRevoke, chat: chat.String(), target: "ORIGINAL"},
			ok:   true,
		},
		{
			name: "ordinary message",
			e:    event(&waE2E.Message{Conversation: proto.String("hi")}, false),
		},
		{
			name: "edit flag without an edit protocol message",
			e:    event(&waE2E.Message{Conversation: proto.String("odd")}, true),
		},
		{
			name: "other protocol message",
			e: event(&waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
				Type: waE2E.ProtocolMessage_EPHEMERAL_SETTING.Enum(), Key: key,
			}}, false),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toChange(tt.e, types.JID.ToNonAD)
			if ok != tt.ok || got != tt.want {
				t.Errorf("toChange = %+v, %v; want %+v, %v", got, ok, tt.want, tt.ok)
			}
			// Edits and deletions must never be archived as new messages.
			if tt.ok {
				if _, archived := toMessage(tt.e, types.JID.ToNonAD); archived {
					t.Error("change would also be archived as a message")
				}
			}
		})
	}
}

func TestSecretEditChange(t *testing.T) {
	chat := types.NewJID("31600000001", types.DefaultUserServer)
	at := time.Unix(1_700_000_000, 0)
	e := &events.Message{
		Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: chat}, ID: "WRAPPER", Timestamp: at},
		Message: &waE2E.Message{SecretEncryptedMessage: &waE2E.SecretEncryptedMessage{
			SecretEncType:    waE2E.SecretEncryptedMessage_MESSAGE_EDIT.Enum(),
			TargetMessageKey: &waCommon.MessageKey{ID: proto.String("ORIGINAL")},
		}},
	}
	want := change{kind: changeEdit, chat: chat.String(), target: "ORIGINAL", text: "mango", at: at.Unix()}

	// Plaintext is the new content itself.
	got, ok := secretEditChange(e, &waE2E.Message{Conversation: proto.String("mango")}, types.JID.ToNonAD)
	if !ok || got != want {
		t.Errorf("plain content: %+v %v, want %+v", got, ok, want)
	}

	// Plaintext wraps the content in a MESSAGE_EDIT protocol message with its own timestamp.
	wrapped := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
		EditedMessage: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("mango")}},
		TimestampMS:   proto.Int64(1_700_000_050_000),
	}}
	want.at = 1_700_000_050
	if got, ok := secretEditChange(e, wrapped, types.JID.ToNonAD); !ok || got != want {
		t.Errorf("wrapped content: %+v %v, want %+v", got, ok, want)
	}

	if _, ok := secretEditChange(e, &waE2E.Message{}, types.JID.ToNonAD); ok {
		t.Error("empty plaintext: want no change")
	}
	e.Message.SecretEncryptedMessage.TargetMessageKey = nil
	if _, ok := secretEditChange(e, &waE2E.Message{Conversation: proto.String("x")}, types.JID.ToNonAD); ok {
		t.Error("missing target: want no change")
	}
}

func TestMediaRefRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	tests := []struct {
		kind     string
		msg      *waE2E.Message
		fileName string
	}{
		{"image", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{DirectPath: proto.String("/v/img"), MediaKey: key, Mimetype: proto.String("image/jpeg"), FileLength: proto.Uint64(1234)}}, ""},
		{"video", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{DirectPath: proto.String("/v/vid"), MediaKey: key, Mimetype: proto.String("video/mp4")}}, ""},
		{"audio", &waE2E.Message{AudioMessage: &waE2E.AudioMessage{DirectPath: proto.String("/v/aud"), MediaKey: key, Mimetype: proto.String("audio/ogg")}}, ""},
		{"document", &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{DirectPath: proto.String("/v/doc"), MediaKey: key, Mimetype: proto.String("application/pdf"), FileName: proto.String("invoice.pdf")}}, "invoice.pdf"},
		{"sticker", &waE2E.Message{StickerMessage: &waE2E.StickerMessage{DirectPath: proto.String("/v/stk"), MediaKey: key, Mimetype: proto.String("image/webp")}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			_, kind := messageContent(tt.msg)
			if kind != tt.kind {
				t.Fatalf("messageContent kind = %q, want %q", kind, tt.kind)
			}
			ref := mediaRef(tt.msg)
			if len(ref) == 0 {
				t.Fatal("no ref")
			}
			sub, fileName, err := decodeMediaRef(kind, ref)
			if err != nil {
				t.Fatal(err)
			}
			if sub.GetDirectPath() == "" || string(sub.GetMediaKey()) != string(key) || sub.GetMimetype() == "" || fileName != tt.fileName {
				t.Errorf("decoded %+v file %q", sub, fileName)
			}
		})
	}
	if ref := mediaRef(&waE2E.Message{Conversation: proto.String("text")}); ref != nil {
		t.Errorf("text message has a ref: %v", ref)
	}
	if ref := mediaRef(&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("link https://x")}}); len(ref) == 0 {
		t.Error("extended text message has no ref")
	}
	if _, _, err := decodeMediaRef("hologram", []byte{1}); err == nil {
		t.Error("unknown kind: want error")
	}
}

func TestMediaGone(t *testing.T) {
	for _, err := range []error{whatsmeow.ErrMediaDownloadFailedWith403, whatsmeow.ErrMediaDownloadFailedWith404, whatsmeow.ErrMediaDownloadFailedWith410,
		fmt.Errorf("wrapped: %w", whatsmeow.ErrMediaDownloadFailedWith403)} {
		if !mediaGone(err) {
			t.Errorf("mediaGone(%v) = false", err)
		}
	}
	if mediaGone(errors.New("connection reset")) || mediaGone(nil) {
		t.Error("transient errors must not count as gone")
	}
}
