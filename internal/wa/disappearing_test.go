// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// timerOf returns a chat's stored disappearing timer, and whether the chat
// is in the archive at all.
func timerOf(t *testing.T, c *Client, chat types.JID) (int64, bool) {
	t.Helper()
	chats, err := c.store.ChatsByJID(context.Background(), []string{chat.String()})
	if err != nil {
		t.Fatal(err)
	}
	ch, ok := chats[chat.String()]
	return ch.DisappearingSeconds, ok
}

func setting(secs uint32) *waE2E.Message {
	return &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type:                waE2E.ProtocolMessage_EPHEMERAL_SETTING.Enum(),
		EphemeralExpiration: proto.Uint32(secs),
	}}
}

func TestOnMessageRecordsTimerSetting(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	// From Alice's LID: recorded under her number, and not archived as a message.
	c.onMessage(liveMessage(aliceLID, aliceLID, "T1", false, "", setting(604800)))
	if secs, _ := timerOf(t, c, alicePN); secs != 604800 {
		t.Errorf("timer = %d, want 7 days", secs)
	}
	if _, err := c.store.MessageByID(context.Background(), alicePN.String(), "T1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("setting archived as a message: err %v", err)
	}
	c.onMessage(liveMessage(alicePN, ownJID.ToNonAD(), "T2", true, "", setting(0)))
	if secs, _ := timerOf(t, c, alicePN); secs != 0 {
		t.Errorf("timer after turning it off = %d", secs)
	}
}

func TestOnHistorySyncRecordsTimer(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	c.onHistorySync(historySync(waHistorySync.HistorySync_INITIAL_BOOTSTRAP,
		&waHistorySync.Conversation{ID: proto.String(alicePN.String()), EphemeralExpiration: proto.Uint32(86400),
			Messages: []*waHistorySync.HistorySyncMsg{webMsg("M1", false, 100, "", text("hi"))}},
		&waHistorySync.Conversation{ID: proto.String(bobPN.String()),
			Messages: []*waHistorySync.HistorySyncMsg{webMsg("B1", false, 100, "", text("hey"))}},
	))
	if secs, _ := timerOf(t, c, alicePN); secs != 86400 {
		t.Errorf("Alice's timer = %d, want 24h", secs)
	}
	if secs, ok := timerOf(t, c, bobPN); !ok || secs != 0 {
		t.Errorf("Bob's timer = %d (archived %v), want 0", secs, ok)
	}
}

func TestGroupTimers(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	archive(t, c, incoming(testGroup, alicePN, "G1", 100, "hello group"))
	other := types.NewJID("120363000000000099", types.GroupServer)

	// A live change.
	c.onGroupInfo(&events.GroupInfo{JID: testGroup, Ephemeral: &types.GroupEphemeral{IsEphemeral: true, DisappearingTimer: 7776000}})
	if secs, _ := timerOf(t, c, testGroup); secs != 7776000 {
		t.Errorf("after live change = %d, want 90 days", secs)
	}
	c.onGroupInfo(&events.GroupInfo{JID: other, Ephemeral: &types.GroupEphemeral{IsEphemeral: true, DisappearingTimer: 86400}})
	if _, ok := timerOf(t, c, other); ok {
		t.Error("a group with nothing archived was added by a live change")
	}

	// The connect-time sync: the archived group's timer is turned off; a
	// group with nothing archived is not added.
	fake.joined = []*types.GroupInfo{
		{JID: testGroup},
		{JID: other, GroupEphemeral: types.GroupEphemeral{IsEphemeral: true, DisappearingTimer: 86400}},
	}
	c.syncGroupNames(ctx)
	if secs, _ := timerOf(t, c, testGroup); secs != 0 {
		t.Errorf("after sync = %d, want off", secs)
	}
	if _, ok := timerOf(t, c, other); ok {
		t.Error("a group with nothing archived was added by the timer sync")
	}
}

func TestSetDisappearingTimer(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	if err := c.SetDisappearingTimer(context.Background(), alicePN, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if want := []timerCall{{alicePN, 24 * time.Hour}}; !slices.Equal(fake.timers, want) {
		t.Errorf("timers = %v, want %v", fake.timers, want)
	}
	fake.loggedIn = false
	if err := c.SetDisappearingTimer(context.Background(), alicePN, 0); !errors.Is(err, errNotLoggedIn) {
		t.Errorf("logged out: err %v", err)
	}
}
