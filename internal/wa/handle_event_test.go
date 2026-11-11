// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// TestHandleEventRoutes checks that each event whatsmeow delivers reaches
// the archive, waiting for the background work some of them start. Group
// messages are left to the handler tests.
func TestHandleEventRoutes(t *testing.T) {
	ctx := context.Background()
	at := time.Unix(200, 0)
	call := types.BasicCallMeta{From: alicePN, CallID: "C1", Timestamp: at}

	tests := []struct {
		name  string
		setup func(*testing.T, *Client)
		evts  []any
		check func(*testing.T, *Client)
	}{
		{
			name: "message",
			evts: []any{liveMessage(alicePN, alicePN, "M1", false, "Alice", text("hi"))},
			check: func(t *testing.T, c *Client) {
				if m := archived(t, c, alicePN, "M1"); m.Text != "hi" {
					t.Errorf("M1 = %+v", m)
				}
			},
		},
		{
			name: "receipt",
			setup: func(t *testing.T, c *Client) {
				archive(t, c, store.Message{ChatJID: alicePN.String(), MessageID: "S1",
					SenderJID: ownJID.ToNonAD().String(), Timestamp: 100, FromMe: true, Text: "x"})
			},
			evts: []any{&events.Receipt{
				MessageSource: types.MessageSource{Chat: alicePN, Sender: alicePN},
				MessageIDs:    []types.MessageID{"S1"}, Timestamp: at, Type: types.ReceiptTypeRead,
			}},
			check: func(t *testing.T, c *Client) {
				if s := archived(t, c, alicePN, "S1").Status; s != store.StatusRead {
					t.Errorf("status = %q, want read", s)
				}
			},
		},
		{
			name: "archive, pin and mute",
			setup: func(t *testing.T, c *Client) {
				if err := c.store.Save(ctx, []store.Chat{{JID: alicePN.String()}}, nil); err != nil {
					t.Fatal(err)
				}
			},
			evts: []any{
				&events.Archive{JID: alicePN, Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(true)}},
				&events.Pin{JID: alicePN, Action: &waSyncAction.PinAction{Pinned: proto.Bool(true)}},
				&events.Mute{JID: alicePN, Action: &waSyncAction.MuteAction{Muted: proto.Bool(true)}},
			},
			check: func(t *testing.T, c *Client) {
				chats, err := c.store.ChatsByJID(ctx, []string{alicePN.String()})
				if err != nil {
					t.Fatal(err)
				}
				if ch := chats[alicePN.String()]; !ch.Archived || !ch.Pinned || ch.MutedUntil != -1 {
					t.Errorf("chat = %+v, want archived, pinned and muted forever", ch)
				}
			},
		},
		{
			name: "call offer then reject",
			evts: []any{
				&events.CallOffer{BasicCallMeta: call},
				&events.CallReject{BasicCallMeta: call},
			},
			check: func(t *testing.T, c *Client) {
				got, err := c.store.CallByID(ctx, "C1")
				if err != nil || got.Status != store.CallRejected || got.CallerJID != alicePN.String() {
					t.Errorf("call = %+v, err %v; want rejected call from alice", got, err)
				}
			},
		},
		{
			name: "call notice; call accepted then terminated",
			evts: []any{
				&events.CallOfferNotice{BasicCallMeta: types.BasicCallMeta{From: alicePN, CallID: "C2", Timestamp: at}},
				&events.CallOffer{BasicCallMeta: types.BasicCallMeta{From: alicePN, CallID: "C3", Timestamp: at}},
				&events.CallAccept{BasicCallMeta: types.BasicCallMeta{From: alicePN, CallID: "C3", Timestamp: at}},
				&events.CallTerminate{BasicCallMeta: types.BasicCallMeta{From: alicePN, CallID: "C3", Timestamp: at.Add(time.Minute)}},
			},
			check: func(t *testing.T, c *Client) {
				if got, err := c.store.CallByID(ctx, "C2"); err != nil || got.Status != store.CallRinging {
					t.Errorf("C2 = %+v, err %v; want ringing", got, err)
				}
				// Ended, not missed, only if the accept got through before the terminate.
				got, err := c.store.CallByID(ctx, "C3")
				if err != nil || got.Status != store.CallEnded || got.EndedAt != at.Add(time.Minute).Unix() {
					t.Errorf("C3 = %+v, err %v; want ended a minute in", got, err)
				}
			},
		},
		{
			name: "joined group, then renamed",
			evts: []any{
				&events.JoinedGroup{GroupInfo: types.GroupInfo{JID: testGroup, GroupName: types.GroupName{Name: "Family"}}},
				&events.GroupInfo{JID: testGroup, Name: &types.GroupName{Name: "Fam"}},
			},
			check: func(t *testing.T, c *Client) {
				if n := chatNames(t, c, testGroup)[testGroup.String()]; n != "Fam" {
					t.Errorf("group name = %q, want Fam", n)
				}
			},
		},
		{
			name: "history sync reconciles chats but not groups",
			setup: func(t *testing.T, c *Client) {
				addressBook(c.device.Contacts.(*memContacts))
				c.cli.(*fakeWA).joined = []*types.GroupInfo{{JID: testGroup, GroupName: types.GroupName{Name: "Family"}}}
				if err := c.store.Save(ctx, []store.Chat{{JID: alicePN.String()}, {JID: testGroup.String()}}, nil); err != nil {
					t.Fatal(err)
				}
			},
			evts: []any{&events.HistorySync{Data: &waHistorySync.HistorySync{}}},
			check: func(t *testing.T, c *Client) {
				want := map[string]string{alicePN.String(): "Alice Saved", testGroup.String(): ""}
				if got := chatNames(t, c, alicePN, testGroup); !reflect.DeepEqual(got, want) {
					t.Errorf("names = %v, want %v", got, want)
				}
			},
		},
		{
			name: "connected reconciles chats and groups",
			setup: func(t *testing.T, c *Client) {
				addressBook(c.device.Contacts.(*memContacts))
				c.cli.(*fakeWA).joined = []*types.GroupInfo{{JID: testGroup, GroupName: types.GroupName{Name: "Family"}}}
				if err := c.store.Save(ctx, []store.Chat{{JID: alicePN.String()}, {JID: testGroup.String()}}, nil); err != nil {
					t.Fatal(err)
				}
			},
			evts: []any{&events.Connected{}},
			check: func(t *testing.T, c *Client) {
				want := map[string]string{alicePN.String(): "Alice Saved", testGroup.String(): "Family"}
				if got := chatNames(t, c, alicePN, testGroup); !reflect.DeepEqual(got, want) {
					t.Errorf("names = %v, want %v", got, want)
				}
			},
		},
		{
			name:  "connection state changes only log",
			evts:  []any{&events.Disconnected{}, &events.LoggedOut{}, struct{}{}},
			check: func(*testing.T, *Client) {},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, _, _ := newTestClient(t)
			if tt.setup != nil {
				tt.setup(t, c)
			}
			for _, e := range tt.evts {
				c.handleEvent(e)
			}
			c.bg.Wait()
			tt.check(t, c)
		})
	}
}

// A burst of history syncs must not cost a reconcile pass each: while one
// pass runs, all further requests fold into a single queued pass.
func TestHistorySyncBurstCoalescesReconcile(t *testing.T) {
	c, _, _, contacts := newTestClient(t)
	contacts.gate = make(chan struct{})
	for range 10 {
		c.handleEvent(&events.HistorySync{Data: &waHistorySync.HistorySync{}})
	}
	close(contacts.gate)
	c.bg.Wait()
	if contacts.calls < 1 || contacts.calls > 2 {
		t.Errorf("10 history syncs ran %d reconcile passes, want 1 or 2", contacts.calls)
	}
}

// whatsmeow keeps delivering queued history syncs after Disconnect; they must
// not start work that would hold up shutdown.
func TestHistorySyncAfterStopStartsNoReconcile(t *testing.T) {
	c, _, _, contacts := newTestClient(t)
	c.Stop()
	c.handleEvent(&events.HistorySync{Data: &waHistorySync.HistorySync{}})
	c.handleEvent(liveMessage(testGroup, alicePN, "G1", false, "Alice", text("hi")))
	c.bg.Wait()
	if contacts.calls != 0 {
		t.Errorf("%d reconcile passes after Stop, want 0", contacts.calls)
	}
	if _, looked := c.groupLookups.Load(testGroup.String()); looked {
		t.Error("group name lookup started after Stop")
	}
}

// whatsmeow marks status updates as group messages; they must not trigger a group lookup.
func TestStatusUpdateStartsNoGroupLookup(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	e := liveMessage(types.StatusBroadcastJID, alicePN, "S1", false, "Alice", text("status"))
	e.Info.IsGroup = true // as whatsmeow sets it for @broadcast
	c.handleEvent(e)
	c.bg.Wait()
	if len(fake.groupInfoCalls) != 0 {
		t.Errorf("GetGroupInfo calls = %v, want none", fake.groupInfoCalls)
	}
}

func TestRetrySave(t *testing.T) {
	ctx := context.Background()
	failing := errors.New("database is closed")

	calls := 0
	err := retrySave(ctx, nil, func() error {
		if calls++; calls < 3 {
			return failing
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Errorf("fails twice: err %v after %d calls, want nil after 3", err, calls)
	}

	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := retrySave(short, nil, func() error { return failing }); !errors.Is(err, failing) || time.Since(start) > time.Second {
		t.Errorf("always failing: %v after %v, want the last error once ctx ends", err, time.Since(start))
	}

	stop := make(chan struct{})
	close(stop)
	calls = 0
	if err := retrySave(ctx, stop, func() error { calls++; return failing }); !errors.Is(err, failing) || calls != 1 {
		t.Errorf("stopped: %v after %d calls, want the error after 1", err, calls)
	}

	// Postgres rejects U+0000 in text on every attempt.
	permanent := fmt.Errorf("save messages: %w", &pgconn.PgError{Code: "22021"})
	bounded, cancelBounded := context.WithTimeout(ctx, time.Second) // without the check, it retries until here
	defer cancelBounded()
	calls = 0
	if err := retrySave(bounded, nil, func() error { calls++; return permanent }); !errors.Is(err, permanent) || calls != 1 {
		t.Errorf("data error: %v after %d calls, want the error after 1", err, calls)
	}
}

// Once Stop begins, a history sync archives no further conversations, so
// Stop does not wait for the rest of a large blob.
func TestHistorySyncStopsOnShutdown(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	c.stopBg()
	c.onHistorySync(historySync(waHistorySync.HistorySync_RECENT,
		&waHistorySync.Conversation{ID: proto.String(alicePN.String()),
			Messages: []*waHistorySync.HistorySyncMsg{webMsg("A1", false, 100, "", text("hi"))}},
		&waHistorySync.Conversation{ID: proto.String(bobPN.String()),
			Messages: []*waHistorySync.HistorySyncMsg{webMsg("B1", false, 101, "", text("hi"))}},
	))
	for _, m := range []struct {
		chat types.JID
		id   string
	}{{alicePN, "A1"}, {bobPN, "B1"}} {
		if _, err := c.store.MessageByID(context.Background(), m.chat.String(), m.id); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s after Stop: %v, want not archived", m.id, err)
		}
	}
}

// Stop must not return while a handler is still writing to the archive,
// which main closes right after.
func TestStopWaitsForHandlers(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	c.groupLookups.Store(testGroup.String(), struct{}{}) // no background name lookup
	c.handleEvent(liveMessage(testGroup, alicePN, "G1", false, "", text("hi")))
	fake.groups[testGroup] = &types.GroupInfo{JID: testGroup, Participants: []types.GroupParticipant{{JID: alicePN}, {JID: bobPN}}}
	fake.groupBlock = make(chan struct{})

	// Bob is not the author, so the handler asks WhatsApp whether he is an admin, and blocks there.
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		c.handleEvent(liveMessage(testGroup, bobPN, "R1", false, "", &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_REVOKE.Enum(),
			Key:  &waCommon.MessageKey{ID: proto.String("G1")},
		}}))
	}()
	for !func() bool { fake.mu.Lock(); defer fake.mu.Unlock(); return len(fake.groupInfoCalls) > 0 }() {
		select {
		case <-handled:
			t.Fatal("handler returned without asking for the group's admins")
		case <-time.After(time.Millisecond):
		}
	}
	stopped := make(chan struct{})
	go func() { c.Stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("Stop returned while a handler was running")
	case <-time.After(100 * time.Millisecond):
	}
	close(fake.groupBlock)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return after the handler did")
	}
	<-handled

	// Events after Stop are dropped.
	c.handleEvent(liveMessage(alicePN, alicePN, "LATE", false, "", text("late")))
	if _, err := c.store.MessageByID(context.Background(), alicePN.String(), "LATE"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("message after Stop: %v, want not archived", err)
	}
}
