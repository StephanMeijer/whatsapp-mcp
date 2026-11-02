// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestSendText(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	m, err := c.SendText(ctx, "+31 6 0000 0001", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 1 || fake.sent[0].to != alicePN || fake.sent[0].msg.GetConversation() != "hello" {
		t.Fatalf("sent = %+v, want hello to %s", fake.sent, alicePN)
	}
	want := store.Message{ChatJID: alicePN.String(), MessageID: "OUT1", SenderJID: "31600000000@s.whatsapp.net",
		Timestamp: sendTime.Unix(), FromMe: true, Text: "hello"}
	if m.ChatJID != want.ChatJID || m.MessageID != want.MessageID || m.SenderJID != want.SenderJID ||
		m.Timestamp != want.Timestamp || !m.FromMe || m.Text != want.Text || m.MediaRef != nil {
		t.Errorf("SendText = %+v, want %+v", m, want)
	}
	if got := archived(t, c, alicePN, "OUT1"); got.Text != "hello" || !got.FromMe {
		t.Errorf("archived %+v", got)
	}
}

// A reply in a LID-addressed group quotes the sender by LID, and the
// archive keeps the quote so a later edit can rebuild it.
func TestSendTextQuotesInLIDGroup(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	fake.groups[testGroup] = &types.GroupInfo{JID: testGroup, AddressingMode: types.AddressingModeLID}
	quoted := incoming(testGroup, alicePN, "Q1", 100, "original")
	archive(t, c, quoted)

	m, err := c.SendText(context.Background(), testGroup.String(), "reply", &quoted)
	if err != nil {
		t.Fatal(err)
	}
	ext := fake.sent[0].msg.GetExtendedTextMessage()
	ci := ext.GetContextInfo()
	if ext.GetText() != "reply" || ci.GetStanzaID() != "Q1" || ci.GetParticipant() != aliceLID.String() ||
		ci.GetQuotedMessage().GetConversation() != "original" {
		t.Errorf("sent %v, want a reply quoting Q1 by %s", ext, aliceLID)
	}
	var ref waE2E.ExtendedTextMessage
	if err := proto.Unmarshal(m.MediaRef, &ref); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&ref, ext) {
		t.Errorf("archived ref = %v, want the sent reply", &ref)
	}
}

func TestSendTextQuotesMedia(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	quoted := store.Message{ChatJID: alicePN.String(), MessageID: "P1", SenderJID: alicePN.String(), Timestamp: 100, MediaType: "image"}
	if _, err := c.SendText(context.Background(), alicePN.String(), "nice", &quoted); err != nil {
		t.Fatal(err)
	}
	ci := fake.sent[0].msg.GetExtendedTextMessage().GetContextInfo()
	if ci.GetQuotedMessage().GetConversation() != "[image]" || ci.GetParticipant() != alicePN.String() {
		t.Errorf("context info = %v", ci)
	}
}

func TestSendTextErrors(t *testing.T) {
	ctx := context.Background()
	t.Run("not logged in", func(t *testing.T) {
		c, fake, _, _ := newTestClient(t)
		fake.loggedIn = false
		if _, err := c.SendText(ctx, alicePN.String(), "hi", nil); err == nil || len(fake.sent) != 0 {
			t.Errorf("err %v, %d sent", err, len(fake.sent))
		}
	})
	t.Run("bad recipient", func(t *testing.T) {
		c, fake, _, _ := newTestClient(t)
		if _, err := c.SendText(ctx, "abc", "hi", nil); err == nil || len(fake.sent) != 0 {
			t.Errorf("err %v, %d sent", err, len(fake.sent))
		}
	})
	t.Run("send fails", func(t *testing.T) {
		c, fake, _, _ := newTestClient(t)
		fake.sendErr = errors.New("offline")
		_, err := c.SendText(ctx, alicePN.String(), "hi", nil)
		if err == nil || !strings.Contains(err.Error(), "send:") {
			t.Errorf("err = %v", err)
		}
		msgs, err := c.store.ListMessages(ctx, alicePN.String(), time.Time{}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) != 0 {
			t.Errorf("archived %d messages after a failed send", len(msgs))
		}
	})
}
