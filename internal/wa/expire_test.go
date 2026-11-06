// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestToMessageRecordsExpiry(t *testing.T) {
	e := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: alicePN, Sender: alicePN},
			ID:            "E1",
			Timestamp:     time.Unix(1_000, 0),
		},
		Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("gone tomorrow"), ContextInfo: &waE2E.ContextInfo{Expiration: proto.Uint32(86400)}}},
	}
	canon := func(j types.JID) types.JID { return j }
	if m, _ := toMessage(e, canon); m.ExpiresAt != 1_000+86400 {
		t.Errorf("ExpiresAt = %d, want sent time plus 24h", m.ExpiresAt)
	}
	e.Message = &waE2E.Message{Conversation: proto.String("stays")}
	if m, _ := toMessage(e, canon); m.ExpiresAt != 0 {
		t.Errorf("ExpiresAt without a timer = %d, want 0", m.ExpiresAt)
	}
}

func TestExpireDisappearing(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	due := incoming(alicePN, alicePN, "D1", 100, "secret")
	due.ExpiresAt = time.Now().Add(-time.Minute).Unix()
	archive(t, c, due, incoming(alicePN, alicePN, "K1", 100, "kept"))

	c.ExpireDisappearing(time.Hour)
	ctx := context.Background()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := c.store.MessageByID(ctx, alicePN.String(), "D1"); errors.Is(err, store.ErrNotFound) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the expired message was not removed by the first run")
		}
		time.Sleep(10 * time.Millisecond)
	}
	archived(t, c, alicePN, "K1")

	stopped := make(chan struct{})
	go func() { c.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not end the expiry worker")
	}
}
