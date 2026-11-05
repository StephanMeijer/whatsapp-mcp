// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"slices"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// phoneVCard is shaped like a card shared from the WhatsApp phone app.
const phoneVCard = "BEGIN:VCARD\r\nVERSION:3.0\r\nN:Doe;Jane;;;\r\nFN:Jane Doe\r\n" +
	"item1.TEL;waid=31612345678:+31 6 12345678\r\nitem1.X-ABLabel:Mobile\r\n" +
	"TEL;type=WORK:+31 20 1234567\r\nEND:VCARD"

func TestVCardPhones(t *testing.T) {
	if got, want := vcardPhones(phoneVCard), []string{"+31 6 12345678", "+31 20 1234567"}; !slices.Equal(got, want) {
		t.Errorf("vcardPhones = %q, want %q", got, want)
	}
	// A dot in a parameter must not be taken for a group prefix.
	if got, want := vcardPhones("TEL;X-ABLabel=St. Mary's:+44 20 7946 0000"), []string{"+44 20 7946 0000"}; !slices.Equal(got, want) {
		t.Errorf("vcardPhones with a dotted parameter = %q, want %q", got, want)
	}
	if got := vcardPhones("BEGIN:VCARD\nFN:No Number\nEND:VCARD"); got != nil {
		t.Errorf("vcardPhones without TEL = %q", got)
	}
}

func TestMessageContentContacts(t *testing.T) {
	jane := &waE2E.ContactMessage{DisplayName: proto.String("Jane Doe"), Vcard: proto.String(phoneVCard)}
	bare := &waE2E.ContactMessage{DisplayName: proto.String("No Number")}
	tests := []struct {
		name string
		msg  *waE2E.Message
		text string
	}{
		{"one card", &waE2E.Message{ContactMessage: jane}, "Jane Doe: +31 6 12345678, +31 20 1234567"},
		{"card without number", &waE2E.Message{ContactMessage: bare}, "No Number"},
		{"several cards", &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{
			DisplayName: proto.String("2 contacts"), Contacts: []*waE2E.ContactMessage{jane, bare},
		}}, "Jane Doe: +31 6 12345678, +31 20 1234567\nNo Number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, kind := messageContent(tt.msg)
			if text != tt.text || kind != store.MediaContact {
				t.Errorf("messageContent = %q, %q; want %q, contact", text, kind, tt.text)
			}
		})
	}
}

func TestSendContact(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	m, err := c.SendContact(context.Background(), "+31600000001", "Doe, Jane;\nCEO", "0031 6 12345678")
	if err != nil {
		t.Fatal(err)
	}
	card := fake.sent[0].msg.GetContactMessage()
	want := "BEGIN:VCARD\nVERSION:3.0\nFN:" + `Doe\, Jane\;\nCEO` + "\nTEL;type=CELL;waid=31612345678:+31612345678\nEND:VCARD"
	if card.GetVcard() != want || card.GetDisplayName() != "Doe, Jane;\nCEO" {
		t.Errorf("sent card %q / %q, want vCard %q", card.GetDisplayName(), card.GetVcard(), want)
	}
	if m.MediaType != store.MediaContact || m.Text != "Doe, Jane;\nCEO: +31612345678" {
		t.Errorf("archived %+v", m)
	}
}

func TestSendContactRejectsBadInput(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	for _, in := range [][2]string{{"", "+31612345678"}, {"  ", "+31612345678"}, {"Jane", "06 12345678"}, {"Jane", "call me"}} {
		if _, err := c.SendContact(context.Background(), "+31600000001", in[0], in[1]); err == nil {
			t.Errorf("SendContact(%q, %q): want error", in[0], in[1])
		}
	}
	if len(fake.sent) != 0 {
		t.Errorf("bad input still sent: %d", len(fake.sent))
	}
}

func TestContactHasNoMediaOrEdit(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	m := store.Message{MessageID: "C1", FromMe: true, MediaType: store.MediaContact, Timestamp: sendTime.Unix()}
	if _, err := c.Media(context.Background(), m); err == nil {
		t.Error("Media: want error for a contact card")
	}
	if err := editable(m, "new", sendTime); err == nil {
		t.Error("editable: want error for a contact card")
	}
}
