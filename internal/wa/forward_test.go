// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestForwardText(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	src := incoming(alicePN, alicePN, "M1", 100, "see you at 8")
	src.Forwarded, src.ForwardingScore = true, 4

	got, err := c.Forward(context.Background(), bobPN.String(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 1 || fake.sent[0].to != bobPN {
		t.Fatalf("sent = %+v", fake.sent)
	}
	ext := fake.sent[0].msg.GetExtendedTextMessage()
	if ext.GetText() != "see you at 8" || !ext.GetContextInfo().GetIsForwarded() || ext.GetContextInfo().GetForwardingScore() != 5 {
		t.Errorf("sent message = %v, want the text forwarded with score 5", fake.sent[0].msg)
	}
	if !got.Forwarded || got.ForwardingScore != 5 || got.ChatJID != bobPN.String() || !got.FromMe {
		t.Errorf("returned = %+v", got)
	}
	if a := archived(t, c, bobPN, got.MessageID); !a.Forwarded || a.ForwardingScore != 5 {
		t.Errorf("archived = %+v", a)
	}
}

func TestForwardMedia(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ref := mediaRef(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		DirectPath: proto.String("/v/img"), Mimetype: proto.String("image/jpeg"), FileLength: proto.Uint64(4), Caption: proto.String("the view"),
	}}, false)
	src := store.Message{ChatJID: alicePN.String(), MessageID: "P1", MediaType: store.MediaImage, MediaRef: ref, Text: "the view"}
	fake.download = []byte("\xff\xd8\xff\xe0")

	if _, err := c.Forward(context.Background(), bobPN.String(), src); err != nil {
		t.Fatal(err)
	}
	if len(fake.uploads) != 1 || fake.uploads[0] != whatsmeow.MediaImage {
		t.Errorf("uploads = %v, want one image", fake.uploads)
	}
	img := fake.sent[0].msg.GetImageMessage()
	if img == nil || img.GetCaption() != "the view" || !img.GetContextInfo().GetIsForwarded() || img.GetContextInfo().GetForwardingScore() != 1 {
		t.Errorf("sent message = %v, want the image forwarded with its caption and score 1", fake.sent[0].msg)
	}

	// A file gone from WhatsApp's servers is not sent.
	fake.downloadErr = whatsmeow.ErrMediaDownloadFailedWith410
	if _, err := c.Forward(context.Background(), bobPN.String(), src); err == nil || !strings.Contains(err.Error(), "no longer on WhatsApp's servers") {
		t.Errorf("expired media: err = %v", err)
	}
	if len(fake.sent) != 1 {
		t.Errorf("expired media was sent: %d messages", len(fake.sent))
	}
}

// A document is archived as its caption, else its title, else its file name;
// only a real caption is forwarded as one.
func TestForwardDocumentCaption(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ref := mediaRef(&waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		DirectPath: proto.String("/v/doc"), Mimetype: proto.String("application/pdf"), FileLength: proto.Uint64(4),
		Title: proto.String("Q3 report"), FileName: proto.String("q3.pdf"),
	}}, false)
	src := store.Message{ChatJID: alicePN.String(), MessageID: "F1", MediaType: store.MediaDocument, MediaRef: ref, Text: "Q3 report"}
	fake.download = []byte("%PDF")

	if _, err := c.Forward(context.Background(), bobPN.String(), src); err != nil {
		t.Fatal(err)
	}
	if doc := fake.sent[0].msg.GetDocumentMessage(); doc == nil || doc.Caption != nil || doc.GetFileName() != "q3.pdf" {
		t.Errorf("sent message = %v, want q3.pdf with no caption", fake.sent[0].msg)
	}
}

func TestForwardVoiceNote(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ref := mediaRef(&waE2E.Message{AudioMessage: &waE2E.AudioMessage{
		DirectPath: proto.String("/v/ptt"), Mimetype: proto.String("audio/ogg; codecs=opus"), FileLength: proto.Uint64(4), PTT: proto.Bool(true),
	}}, false)
	src := store.Message{ChatJID: alicePN.String(), MessageID: "V1", MediaType: store.MediaAudio, MediaRef: ref}
	fake.download = []byte("OggS")

	if _, err := c.Forward(context.Background(), bobPN.String(), src); err != nil {
		t.Fatal(err)
	}
	if !fake.sent[0].msg.GetAudioMessage().GetPTT() {
		t.Errorf("sent message = %v, want a voice note", fake.sent[0].msg)
	}
}

func TestForwardRefuses(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	poll := store.Message{ChatJID: alicePN.String(), MessageID: "Q1", MediaType: store.MediaPoll, Text: "Lunch?"}
	if _, err := c.Forward(context.Background(), bobPN.String(), poll); err == nil || !strings.Contains(err.Error(), "poll can't be forwarded") {
		t.Errorf("poll: err = %v", err)
	}
	gone := incoming(alicePN, alicePN, "D1", 100, "")
	gone.Deleted = true
	if _, err := c.Forward(context.Background(), bobPN.String(), gone); err == nil {
		t.Error("deleted message forwarded")
	}
	fake.loggedIn = false
	if _, err := c.Forward(context.Background(), bobPN.String(), incoming(alicePN, alicePN, "M1", 100, "hi")); err == nil {
		t.Error("forwarded while logged out")
	}
	if len(fake.sent) != 0 {
		t.Errorf("sent = %v, want nothing", fake.sent)
	}
}

func TestSetContextInfo(t *testing.T) {
	ci := &waE2E.ContextInfo{IsForwarded: proto.Bool(true)}
	doc := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("a.pdf")}}
	if !setContextInfo(doc, ci) || !doc.GetDocumentMessage().GetContextInfo().GetIsForwarded() {
		t.Errorf("document = %v", doc)
	}
	if setContextInfo(&waE2E.Message{Conversation: proto.String("hi")}, ci) {
		t.Error("a plain conversation took a ContextInfo")
	}
}
