// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// TestHandleEventRoutes checks that each event whatsmeow delivers reaches
// the archive. Events whose handling starts background work (history sync,
// connect, messages in groups) are left to the handler tests.
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
			tt.check(t, c)
		})
	}
}
