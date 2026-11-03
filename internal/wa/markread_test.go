// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// markReadChat archives a group chat with two senders, one of the
// account's own messages and a deleted one.
func markReadChat(t *testing.T, c *Client) {
	t.Helper()
	carol := types.NewJID("31600000003", types.DefaultUserServer)
	own := store.Message{ChatJID: testGroup.String(), MessageID: "O1", SenderJID: ownJID.ToNonAD().String(), Timestamp: 103, FromMe: true, Text: "mine"}
	archive(t, c,
		incoming(testGroup, alicePN, "A1", 100, "one"),
		incoming(testGroup, alicePN, "A2", 101, "two"),
		incoming(testGroup, bobPN, "B1", 102, "three"),
		own,
		incoming(testGroup, carol, "D1", 104, "gone"),
	)
	if _, err := c.store.MarkDeleted(context.Background(), testGroup.String(), "D1"); err != nil {
		t.Fatal(err)
	}
}

func TestMarkChatReadBatchesPerSender(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	fake.groups[testGroup] = &types.GroupInfo{JID: testGroup, AddressingMode: types.AddressingModePN}
	markReadChat(t, c)

	if err := c.MarkChatRead(context.Background(), testGroup); err != nil {
		t.Fatal(err)
	}
	// Newest first, one batch per sender, each at its newest message.
	want := []readCall{
		{ids: []types.MessageID{"B1"}, latest: time.Unix(102, 0), chat: testGroup, sender: bobPN},
		{ids: []types.MessageID{"A2", "A1"}, latest: time.Unix(101, 0), chat: testGroup, sender: alicePN},
	}
	if !reflect.DeepEqual(fake.reads, want) {
		t.Errorf("read receipts = %+v\nwant %+v", fake.reads, want)
	}
	if len(fake.patches) != 1 {
		t.Fatalf("%d app state patches, want 1", len(fake.patches))
	}
	mut := fake.patches[0].Mutations[0]
	if !reflect.DeepEqual(mut.Index, []string{appstate.IndexMarkChatAsRead, testGroup.String()}) {
		t.Errorf("patch index = %v", mut.Index)
	}
	rng := mut.Value.GetMarkChatAsReadAction().GetMessageRange()
	key := rng.GetMessages()[0].GetKey()
	if rng.GetLastMessageTimestamp() != 102 || key.GetID() != "B1" || key.GetParticipant() != bobPN.String() || key.GetFromMe() {
		t.Errorf("message range = %v, want B1 from %s at 102", rng, bobPN)
	}
	if len(fake.groupInfoCalls) != 1 {
		t.Errorf("group info looked up %d times, want once (cached)", len(fake.groupInfoCalls))
	}
}

// In a group that addresses members by LID, receipts go to the LID where it
// is known and to the phone number otherwise.
func TestMarkChatReadInLIDGroup(t *testing.T) {
	c, fake, lids, _ := newTestClient(t)
	fake.groups[testGroup] = &types.GroupInfo{JID: testGroup, AddressingMode: types.AddressingModeLID}
	delete(lids.pnByLID, aliceLID.User)
	markReadChat(t, c)

	if err := c.MarkChatRead(context.Background(), testGroup); err != nil {
		t.Fatal(err)
	}
	if len(fake.reads) != 2 || fake.reads[0].sender != bobLID || fake.reads[1].sender != alicePN {
		t.Errorf("read receipts = %+v, want bob by LID and alice by number", fake.reads)
	}
	key := fake.patches[0].Mutations[0].Value.GetMarkChatAsReadAction().GetMessageRange().GetMessages()[0].GetKey()
	if key.GetParticipant() != bobLID.String() {
		t.Errorf("last message key participant = %s, want %s", key.GetParticipant(), bobLID)
	}
}

func TestMarkChatReadEmptyChat(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	if err := c.MarkChatRead(context.Background(), alicePN); err != nil {
		t.Fatal(err)
	}
	if len(fake.reads) != 0 || len(fake.patches) != 1 {
		t.Errorf("reads %d, patches %d; want 0 and 1", len(fake.reads), len(fake.patches))
	}
	rng := fake.patches[0].Mutations[0].Value.GetMarkChatAsReadAction().GetMessageRange()
	if len(rng.GetMessages()) != 0 {
		t.Errorf("message range for an empty chat = %v", rng)
	}
}

func TestMarkChatReadErrors(t *testing.T) {
	t.Run("not logged in", func(t *testing.T) {
		c, fake, _, _ := newTestClient(t)
		fake.loggedIn = false
		if err := c.MarkChatRead(context.Background(), alicePN); err == nil {
			t.Error("want an error")
		}
		if len(fake.reads)+len(fake.patches) != 0 {
			t.Error("calls made while logged out")
		}
	})
	t.Run("receipts fail", func(t *testing.T) {
		c, fake, _, _ := newTestClient(t)
		archive(t, c, incoming(alicePN, alicePN, "A1", 100, "hi"))
		fake.readErr = errors.New("offline")
		err := c.MarkChatRead(context.Background(), alicePN)
		if err == nil || !strings.Contains(err.Error(), "send read receipts") {
			t.Errorf("err = %v", err)
		}
		if len(fake.patches) != 0 {
			t.Error("chat marked read although receipts failed")
		}
	})
	t.Run("app state fails", func(t *testing.T) {
		c, fake, _, _ := newTestClient(t)
		fake.appStateErrAt = 1
		if err := c.MarkChatRead(context.Background(), alicePN); err == nil || !strings.Contains(err.Error(), "mark chat as read") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("group info fails and is retried", func(t *testing.T) {
		c, fake, _, _ := newTestClient(t)
		fake.groupErr = errors.New("offline")
		markReadChat(t, c)
		for range 2 {
			if err := c.MarkChatRead(context.Background(), testGroup); err != nil {
				t.Fatal(err)
			}
		}
		// Three lookups per call (two senders and the last message), none cached.
		if len(fake.groupInfoCalls) != 6 {
			t.Errorf("group info looked up %d times, want 6", len(fake.groupInfoCalls))
		}
		if fake.reads[0].sender != bobPN {
			t.Errorf("sender without group info = %s, want the phone number", fake.reads[0].sender)
		}
	})
}
