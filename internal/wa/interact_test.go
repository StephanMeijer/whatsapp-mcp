// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestReplyMessage(t *testing.T) {
	sender := types.NewJID("31600000001", types.DefaultUserServer)
	msg := replyMessage("sounds good", store.Message{MessageID: "ORIG", Text: "dinner at 8?"}, sender)
	ext := msg.GetExtendedTextMessage()
	ci := ext.GetContextInfo()
	if ext.GetText() != "sounds good" || ci.GetStanzaID() != "ORIG" || ci.GetParticipant() != sender.String() ||
		ci.GetQuotedMessage().GetConversation() != "dinner at 8?" {
		t.Errorf("reply = %v", msg)
	}

	media := replyMessage("nice", store.Message{MessageID: "PIC", MediaType: "image"}, sender)
	if got := media.GetExtendedTextMessage().GetContextInfo().GetQuotedMessage().GetConversation(); got != "[image]" {
		t.Errorf("quote of a captionless image = %q, want [image]", got)
	}
}

func TestChooseWireSender(t *testing.T) {
	own := types.NewJID("31600000000", types.DefaultUserServer)
	pn := types.NewJID("31600000001", types.DefaultUserServer)
	lid := types.NewJID("111", types.HiddenUserServer)
	dm := pn
	group := types.NewJID("120363000000000000", types.GroupServer)

	tests := []struct {
		name      string
		chat      types.JID
		fromMe    bool
		lidGroup  bool
		senderLID types.JID
		want      types.JID
	}{
		{"own message", group, true, true, lid, own},
		{"direct chat", dm, false, false, types.JID{}, pn},
		{"phone-number group", group, false, false, lid, pn},
		{"LID group with known LID", group, false, true, lid, lid},
		{"LID group without known LID", group, false, true, types.JID{}, pn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chooseWireSender(tt.chat, pn, own, tt.fromMe, tt.lidGroup, tt.senderLID); got != tt.want {
				t.Errorf("chooseWireSender = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestEditable(t *testing.T) {
	now := time.Unix(10_000, 0)
	sent := now.Add(-time.Minute).Unix()
	own := store.Message{MessageID: "M", FromMe: true, Timestamp: sent}
	withRef := func(m store.Message, kind string) store.Message {
		m.MediaType, m.MediaRef = kind, []byte{1}
		return m
	}
	if err := editable(own, "fixed", now); err != nil {
		t.Errorf("own recent text: %v", err)
	}
	if err := editable(withRef(own, "image"), "new caption", now); err != nil {
		t.Errorf("own recent image: %v", err)
	}
	atLimit := own
	atLimit.Timestamp = now.Add(-whatsmeow.EditWindow).Unix()
	if err := editable(atLimit, "fixed", now); err != nil {
		t.Errorf("at the edit window: %v", err)
	}
	for name, m := range map[string]store.Message{
		"someone else's": {MessageID: "M", Timestamp: sent},
		"deleted":        {MessageID: "M", FromMe: true, Deleted: true, Timestamp: sent},
		"too old":        {MessageID: "M", FromMe: true, Timestamp: now.Add(-whatsmeow.EditWindow - time.Second).Unix()},
		"audio":          withRef(own, "audio"),
		"sticker":        withRef(own, "sticker"),
		"image, no ref":  {MessageID: "M", FromMe: true, Timestamp: sent, MediaType: "image"},
	} {
		if err := editable(m, "fixed", now); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if err := editable(own, "", now); err == nil {
		t.Error("empty text: want error")
	}
}

func TestEditContent(t *testing.T) {
	msg, err := editContent(store.Message{MessageID: "M"}, "fixed")
	if err != nil || msg.GetConversation() != "fixed" {
		t.Errorf("text edit = %v, %v", msg, err)
	}

	ref, err := proto.Marshal(&waE2E.ImageMessage{URL: proto.String("https://mmg/x"), Mimetype: proto.String("image/jpeg"), Caption: proto.String("old")})
	if err != nil {
		t.Fatal(err)
	}
	msg, err = editContent(store.Message{MessageID: "M", MediaType: "image", MediaRef: ref}, "new caption")
	img := msg.GetImageMessage()
	if err != nil || img.GetCaption() != "new caption" || img.GetURL() != "https://mmg/x" || img.GetMimetype() != "image/jpeg" {
		t.Errorf("caption edit = %v, %v", msg, err)
	}
	if text, kind := messageContent(msg); text != "new caption" || kind != "image" {
		t.Errorf("archived as %q/%q", text, kind)
	}
}

func TestEditContentKeepsReplyQuote(t *testing.T) {
	sender := types.JID{User: "31612345678", Server: types.DefaultUserServer}
	reply := replyMessage("sounds good", store.Message{MessageID: "ORIG", Text: "dinner at 8?"}, sender)
	ref := mediaRef(reply)
	if len(ref) == 0 {
		t.Fatal("a reply has no ref")
	}
	msg, err := editContent(store.Message{MessageID: "M", FromMe: true, Text: "sounds good", MediaRef: ref}, "sounds great")
	if err != nil {
		t.Fatal(err)
	}
	ext := msg.GetExtendedTextMessage()
	ci := ext.GetContextInfo()
	if ext.GetText() != "sounds great" || ci.GetStanzaID() != "ORIG" ||
		ci.GetQuotedMessage().GetConversation() != "dinner at 8?" || ci.GetParticipant() != sender.String() {
		t.Errorf("edited reply = %v", msg)
	}
	if text, kind := messageContent(msg); text != "sounds great" || kind != "" {
		t.Errorf("archived as %q/%q", text, kind)
	}
	if _, err := editContent(store.Message{MessageID: "M", MediaRef: []byte{0xff}}, "x"); err == nil {
		t.Error("corrupt ref: want error")
	}
}
