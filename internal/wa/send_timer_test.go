// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestSendsCarryTheChatTimer(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	day := int64(86400)
	if err := c.store.SetChatFlags(ctx, bobPN.String(), store.ChatFlags{DisappearingSeconds: &day}); err != nil {
		t.Fatal(err)
	}

	// Plain text becomes extended text, which can carry the timer.
	m, err := c.SendText(ctx, bobPN.String(), "gone tomorrow", nil)
	if err != nil {
		t.Fatal(err)
	}
	ext := fake.sent[0].msg.GetExtendedTextMessage()
	if ext.GetText() != "gone tomorrow" || ext.GetContextInfo().GetExpiration() != 86400 {
		t.Errorf("sent = %v, want extended text with a 24h expiration", fake.sent[0].msg)
	}
	if want := sendTime.Unix() + day; m.ExpiresAt != want || archived(t, c, bobPN, m.MessageID).ExpiresAt != want {
		t.Errorf("ExpiresAt = %d, want %d", m.ExpiresAt, want)
	}

	// A reply keeps its quote alongside the timer.
	quoted := incoming(bobPN, bobPN, "Q1", 100, "question")
	if _, err := c.SendText(ctx, bobPN.String(), "answer", &quoted); err != nil {
		t.Fatal(err)
	}
	ci := fake.sent[1].msg.GetExtendedTextMessage().GetContextInfo()
	if ci.GetStanzaID() != "Q1" || ci.GetExpiration() != 86400 {
		t.Errorf("reply context = %v, want the quote and the timer", ci)
	}

	// Other content gets the timer on its own sub-message.
	if _, err := c.SendContact(ctx, bobPN.String(), "Alice", "+31600000001"); err != nil {
		t.Fatal(err)
	}
	if exp := fake.sent[2].msg.GetContactMessage().GetContextInfo().GetExpiration(); exp != 86400 {
		t.Errorf("contact card expiration = %d, want 86400", exp)
	}

	// A chat without a timer is sent to as before.
	m, err = c.SendText(ctx, alicePN.String(), "stays", nil)
	if err != nil {
		t.Fatal(err)
	}
	if fake.sent[3].msg.GetConversation() != "stays" || m.ExpiresAt != 0 {
		t.Errorf("no-timer send = %v, ExpiresAt %d; want a plain conversation that never expires", fake.sent[3].msg, m.ExpiresAt)
	}
}
