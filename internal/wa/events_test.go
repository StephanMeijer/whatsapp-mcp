// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// webMsg is one history-sync message. participant is the sender in a group.
func webMsg(id string, fromMe bool, ts uint64, participant string, m *waE2E.Message) *waHistorySync.HistorySyncMsg {
	w := &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{ID: proto.String(id), FromMe: proto.Bool(fromMe)},
		Message:          m,
		MessageTimestamp: proto.Uint64(ts),
	}
	if participant != "" {
		w.Participant = proto.String(participant)
	}
	return &waHistorySync.HistorySyncMsg{Message: w}
}

func text(s string) *waE2E.Message { return &waE2E.Message{Conversation: proto.String(s)} }

func historySync(typ waHistorySync.HistorySync_HistorySyncType, convs ...*waHistorySync.Conversation) *events.HistorySync {
	return &events.HistorySync{
		Data:         &waHistorySync.HistorySync{SyncType: typ.Enum(), Conversations: convs},
		Notification: &waE2E.HistorySyncNotification{},
	}
}

func TestOnHistorySyncArchivesUnderPhoneNumber(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	ctx := context.Background()
	c.onHistorySync(historySync(waHistorySync.HistorySync_INITIAL_BOOTSTRAP,
		// A LID whose number is known: archived under the number.
		&waHistorySync.Conversation{ID: proto.String(aliceLID.String()), Name: proto.String("Alice"),
			Messages: []*waHistorySync.HistorySyncMsg{webMsg("M1", false, 100, "", text("hi"))}},
		// An unmapped LID whose conversation carries its number: archived under that number.
		&waHistorySync.Conversation{ID: proto.String(stranger.String()), PnJID: proto.String("31600000009@s.whatsapp.net"),
			Messages: []*waHistorySync.HistorySyncMsg{webMsg("S1", false, 101, "", text("who is this"))}},
		// A "number" that is another LID is not used.
		&waHistorySync.Conversation{ID: proto.String("888@lid"), PnJID: proto.String("777@lid"),
			Messages: []*waHistorySync.HistorySyncMsg{webMsg("L1", false, 102, "", text("lid"))}},
		// An unparseable chat JID is skipped without stopping the rest.
		&waHistorySync.Conversation{ID: proto.String("1:x@s.whatsapp.net"),
			Messages: []*waHistorySync.HistorySyncMsg{webMsg("X1", false, 103, "", text("bad"))}},
		// A group message without a sender is skipped; the group is kept.
		&waHistorySync.Conversation{ID: proto.String(testGroup.String()), Name: proto.String("Family"),
			Messages: []*waHistorySync.HistorySyncMsg{webMsg("G1", false, 104, "", text("no sender"))}},
	))

	m1 := archived(t, c, alicePN, "M1")
	if m1.SenderJID != alicePN.String() || m1.Text != "hi" {
		t.Errorf("M1 = %+v, want it from %s", m1, alicePN)
	}
	numberChat := types.NewJID("31600000009", types.DefaultUserServer)
	s1 := archived(t, c, numberChat, "S1")
	// The sender is not resolved from the conversation's number; this pins
	// current behaviour.
	if s1.SenderJID != stranger.String() {
		t.Errorf("S1 sender = %s, want %s", s1.SenderJID, stranger)
	}
	archived(t, c, types.NewJID("888", types.HiddenUserServer), "L1")

	names := chatNames(t, c, alicePN, aliceLID, numberChat, stranger, testGroup)
	want := map[string]string{alicePN.String(): "Alice", numberChat.String(): "", testGroup.String(): "Family"}
	for jid, name := range want {
		if got, ok := names[jid]; !ok || got != name {
			t.Errorf("chat %s: name %q (present %v), want %q", jid, got, ok, name)
		}
	}
	for _, jid := range []types.JID{aliceLID, stranger} {
		if _, ok := names[jid.String()]; ok {
			t.Errorf("chat %s archived under its LID", jid)
		}
	}
	if _, err := c.store.MessageByID(ctx, testGroup.String(), "G1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("group message without a sender: err %v, want not found", err)
	}
}

// A message whatsmeow cannot parse is logged and counted, so an archive with
// gaps can be told apart from a sync that had fewer messages.
func TestOnHistorySyncLogsSkippedMessages(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	var buf bytes.Buffer
	c.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c.onHistorySync(historySync(waHistorySync.HistorySync_INITIAL_BOOTSTRAP,
		&waHistorySync.Conversation{ID: proto.String(testGroup.String()),
			Messages: []*waHistorySync.HistorySyncMsg{
				webMsg("G1", false, 104, "", text("no sender")),
				webMsg("G2", false, 105, alicePN.String(), text("ok")),
			}},
	))
	for _, want := range []string{
		`msg="history sync: skip message" chat=` + testGroup.String() + ` id=G1 err=`,
		`msg="history sync archived" type=INITIAL_BOOTSTRAP conversations=1 messages=1 skipped=1`,
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log output:\n%s\nwant a line containing %q", buf.String(), want)
		}
	}
}

// Edits and deletions in a history sync may come before the messages they
// change; they are applied once the conversation is saved.
func TestOnHistorySyncAppliesChangesAfterSave(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	fake.decrypted["M1"] = text("final")
	secretEdit := func(target string) *waE2E.Message {
		return &waE2E.Message{SecretEncryptedMessage: &waE2E.SecretEncryptedMessage{
			TargetMessageKey: &waCommon.MessageKey{ID: proto.String(target)},
			SecretEncType:    waE2E.SecretEncryptedMessage_MESSAGE_EDIT.Enum(),
		}}
	}
	revoke := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_REVOKE.Enum(),
		Key:  &waCommon.MessageKey{ID: proto.String("M2")},
	}}
	c.onHistorySync(historySync(waHistorySync.HistorySync_INITIAL_BOOTSTRAP,
		&waHistorySync.Conversation{ID: proto.String(alicePN.String()), Messages: []*waHistorySync.HistorySyncMsg{
			webMsg("E1", false, 200, "", secretEdit("M1")),
			webMsg("R1", false, 201, "", revoke),
			webMsg("M1", false, 100, "", text("draft")),
			webMsg("M2", false, 101, "", text("oops")),
			webMsg("M3", false, 102, "", text("kept")),
			webMsg("E3", false, 202, "", secretEdit("M3")), // cannot be decrypted
		}},
	))

	if m := archived(t, c, alicePN, "M1"); m.Text != "final" || m.EditedAt != 200 {
		t.Errorf("M1 = %q edited at %d, want the edit applied at 200", m.Text, m.EditedAt)
	}
	if _, err := c.store.MessageByID(ctx, alicePN.String(), "M2"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("M2 after deletion: err %v, want not found", err)
	}
	if m := archived(t, c, alicePN, "M3"); m.Text != "kept" || m.EditedAt != 0 {
		t.Errorf("M3 = %+v, want it unchanged", m)
	}
	for _, id := range []string{"E1", "R1", "E3"} {
		if _, err := c.store.MessageByID(ctx, alicePN.String(), id); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("change %s archived as a message", id)
		}
	}
}

// Documents #62: ParseWebMessage turns a plain MESSAGE_EDIT into the new
// content under the original's ID, so it never reaches toChange, and the
// archive keeps the old text.
func TestOnHistorySyncAppliesPlainEdits(t *testing.T) {
	edit := func(target, to string) *waE2E.Message {
		return &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			Key:           &waCommon.MessageKey{ID: proto.String(target)},
			EditedMessage: text(to),
		}}
	}
	for _, tt := range []struct {
		name string
		wrap func(*waE2E.Message) *waE2E.Message
	}{
		{"bare", func(m *waE2E.Message) *waE2E.Message { return m }},
		{"wrapped in EditedMessage", func(m *waE2E.Message) *waE2E.Message {
			return &waE2E.Message{EditedMessage: &waE2E.FutureProofMessage{Message: m}}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _, _, _ := newTestClient(t)
			c.onHistorySync(historySync(waHistorySync.HistorySync_INITIAL_BOOTSTRAP,
				&waHistorySync.Conversation{ID: proto.String(alicePN.String()), Messages: []*waHistorySync.HistorySyncMsg{
					webMsg("M1", false, 100, "", text("draft")),
					webMsg("E1", false, 200, "", tt.wrap(edit("M1", "edited"))),
					webMsg("M2", false, 300, "", text("unrelated")),
				}},
			))
			if m := archived(t, c, alicePN, "M1"); m.Text != "edited" {
				t.Errorf("M1 = %q, want the edit applied", m.Text)
			}
			if m := archived(t, c, alicePN, "M2"); m.Text != "unrelated" {
				t.Errorf("M2 = %q, want it archived as sent", m.Text)
			}
			if _, err := c.store.MessageByID(context.Background(), alicePN.String(), "E1"); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("the edit itself was archived as a message: err %v", err)
			}
		})
	}
}

func TestOnHistorySyncResolvesOnDemandFetches(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	aliceDone, releaseAlice, err := c.fetches.register(alicePN.String())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseAlice()
	bobDone, releaseBob, err := c.fetches.register(bobPN.String())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseBob()
	c.fetches.setSession(bobPN.String(), "REQ-B")

	// An initial sync answers no request.
	initial := historySync(waHistorySync.HistorySync_INITIAL_BOOTSTRAP,
		&waHistorySync.Conversation{ID: proto.String(alicePN.String()),
			Messages: []*waHistorySync.HistorySyncMsg{webMsg("A0", false, 50, "", text("old"))}})
	initial.Notification.PeerDataRequestSessionID = proto.String("REQ-B")
	c.onHistorySync(initial)
	for name, done := range map[string]<-chan struct{}{"alice": aliceDone, "bob": bobDone} {
		select {
		case <-done:
			t.Errorf("%s resolved by an initial sync", name)
		default:
		}
	}

	// An on-demand answer resolves Alice by her conversation and Bob by
	// his request ID, though it carries nothing for him.
	answer := historySync(waHistorySync.HistorySync_ON_DEMAND,
		&waHistorySync.Conversation{ID: proto.String(alicePN.String()),
			Messages: []*waHistorySync.HistorySyncMsg{webMsg("A1", false, 40, "", text("older"))}})
	answer.Notification.PeerDataRequestSessionID = proto.String("REQ-B")
	c.onHistorySync(answer)
	for name, done := range map[string]<-chan struct{}{"alice": aliceDone, "bob": bobDone} {
		select {
		case <-done:
		default:
			t.Errorf("%s not resolved by the on-demand answer", name)
		}
	}
	archived(t, c, alicePN, "A1")
}
