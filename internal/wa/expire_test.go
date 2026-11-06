// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
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

func TestKeepInChatStopsExpiry(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	ctx := context.Background()
	keep := func(id string, typ waE2E.KeepType) *waE2E.Message {
		return &waE2E.Message{KeepInChatMessage: &waE2E.KeepInChatMessage{
			Key: &waCommon.MessageKey{ID: proto.String(id)}, KeepType: typ.Enum()}}
	}
	timed := func(s string) *waE2E.Message {
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(s), ContextInfo: &waE2E.ContextInfo{Expiration: proto.Uint32(60)}}}
	}

	// Live: a keep clears the timer, and an undo leaves it kept.
	c.onMessage(liveMessage(alicePN, alicePN, "M1", false, "", timed("keep me")))
	c.onMessage(liveMessage(alicePN, alicePN, "K1", false, "", keep("M1", waE2E.KeepType_KEEP_FOR_ALL)))
	c.onMessage(liveMessage(alicePN, alicePN, "U1", false, "", keep("M1", waE2E.KeepType_UNDO_KEEP_FOR_ALL)))
	// History sync: a message whose WebMessageInfo says it was kept.
	hm := webMsg("M2", false, 100, "", timed("kept earlier"))
	hm.Message.KeepInChat = &waWeb.KeepInChat{KeepType: waE2E.KeepType_KEEP_FOR_ALL.Enum()}
	c.onHistorySync(historySync(waHistorySync.HistorySync_INITIAL_BOOTSTRAP,
		&waHistorySync.Conversation{ID: proto.String(alicePN.String()), Messages: []*waHistorySync.HistorySyncMsg{hm}}))

	for _, id := range []string{"M1", "M2"} {
		if m := archived(t, c, alicePN, id); m.ExpiresAt != 0 {
			t.Errorf("%s ExpiresAt = %d, want 0 once kept", id, m.ExpiresAt)
		}
	}
	for _, id := range []string{"K1", "U1"} {
		if _, err := c.store.MessageByID(ctx, alicePN.String(), id); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("keep %s archived as a message", id)
		}
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
