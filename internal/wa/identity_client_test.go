// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// addressBook fills the contact store: Alice saved by number and seen by
// LID, Bob and the stranger known only by their push names.
func addressBook(contacts *memContacts) {
	contacts.all[alicePN] = types.ContactInfo{Found: true, FullName: "Alice Saved"}
	contacts.all[aliceLID] = types.ContactInfo{Found: true, PushName: "alice"}
	contacts.all[bobPN] = types.ContactInfo{Found: true, PushName: "Bob"}
	contacts.all[stranger] = types.ContactInfo{Found: true, PushName: "Stranger"}
}

func TestContacts(t *testing.T) {
	c, _, _, contacts := newTestClient(t)
	addressBook(contacts)
	ctx := context.Background()
	alice := Contact{JID: alicePN.String(), Name: "Alice Saved", PushName: "alice"}
	bob := Contact{JID: bobPN.String(), PushName: "Bob"}
	strangerContact := Contact{JID: stranger.String(), PushName: "Stranger"}
	tests := []struct {
		query string
		want  []Contact
	}{
		// Alice's two entries are one contact, found case-insensitively.
		{"ALI", []Contact{alice}},
		// Sorted by name, then JID; nameless entries first.
		{"", []Contact{bob, strangerContact, alice}},
		{"999@", []Contact{strangerContact}},
		{"nobody", []Contact{}},
	}
	for _, tt := range tests {
		got, err := c.Contacts(ctx, tt.query)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Contacts(%q) = %+v\nwant %+v", tt.query, got, tt.want)
		}
	}
	contacts.err = errors.New("store down")
	if _, err := c.Contacts(ctx, ""); err == nil {
		t.Error("contact store error: want an error")
	}
}

func TestSyncChatNames(t *testing.T) {
	c, _, _, contacts := newTestClient(t)
	addressBook(contacts)
	ctx := context.Background()
	if err := c.store.Save(ctx, []store.Chat{{JID: alicePN.String()}, {JID: bobPN.String()}, {JID: testGroup.String(), Name: "Family"}}, nil); err != nil {
		t.Fatal(err)
	}
	c.syncChatNames(ctx)
	want := map[string]string{alicePN.String(): "Alice Saved", bobPN.String(): "Bob", testGroup.String(): "Family"}
	if got := chatNames(t, c, alicePN, bobPN, testGroup); !reflect.DeepEqual(got, want) {
		t.Errorf("chat names = %v, want %v", got, want)
	}

	// A failing contact store renames nothing.
	contacts.err = errors.New("store down")
	contacts.all[bobPN] = types.ContactInfo{FullName: "Robert"}
	c.syncChatNames(ctx)
	if got := chatNames(t, c, bobPN)[bobPN.String()]; got != "Bob" {
		t.Errorf("bob renamed to %q despite the store error", got)
	}
}

func TestCanonical(t *testing.T) {
	c, _, lids, _ := newTestClient(t)
	ctx := context.Background()
	tests := []struct {
		name string
		in   types.JID
		want types.JID
	}{
		{"number with device", types.NewADJID(alicePN.User, 0, 3), alicePN},
		{"mapped LID with device", types.JID{User: aliceLID.User, Device: 3, Server: types.HiddenUserServer}, alicePN},
		{"unmapped LID", stranger, stranger},
		{"group", testGroup, testGroup},
	}
	for _, tt := range tests {
		if got := c.Canonical(ctx, tt.in); got != tt.want {
			t.Errorf("%s: Canonical(%s) = %s, want %s", tt.name, tt.in, got, tt.want)
		}
	}
	lids.err = errors.New("store down")
	if got := c.Canonical(ctx, aliceLID); got != aliceLID {
		t.Errorf("Canonical with a failing LID store = %s, want the LID itself", got)
	}
}

func TestNames(t *testing.T) {
	c, _, _, contacts := newTestClient(t)
	ctx := context.Background()
	// Alice is in the address book only under her LID.
	contacts.all[aliceLID] = types.ContactInfo{PushName: "alice"}
	got, err := c.Names(ctx, []types.JID{ownJID, alicePN, bobPN})
	if err != nil {
		t.Fatal(err)
	}
	want := map[types.JID]string{ownJID: "Me", alicePN: "alice"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Names = %v, want %v", got, want)
	}
	contacts.err = errors.New("store down")
	if _, err := c.Names(ctx, []types.JID{alicePN}); err == nil {
		t.Error("contact store error: want an error")
	}
}

func TestMergeLIDChats(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	ctx := context.Background()
	archive(t, c,
		incoming(aliceLID, aliceLID, "L1", 100, "by lid"),
		incoming(stranger, stranger, "S1", 101, "unknown"),
	)
	c.mergeLIDChats(ctx)

	if names := chatNames(t, c, aliceLID); len(names) != 0 {
		t.Errorf("chat %s still archived after merging", aliceLID)
	}
	if m := archived(t, c, alicePN, "L1"); m.SenderJID != alicePN.String() {
		t.Errorf("merged message sender = %s, want %s", m.SenderJID, alicePN)
	}
	archived(t, c, stranger, "S1")

	// Nothing left to merge: nothing changes.
	c.mergeLIDChats(ctx)
	msgs, err := c.store.ListMessages(ctx, stranger.String(), time.Time{}, 0)
	if err != nil || len(msgs) != 1 {
		t.Errorf("unmapped chat after a second merge: %d messages, err %v", len(msgs), err)
	}
}

func TestReconcileNamesGroups(t *testing.T) {
	c, fake, _, contacts := newTestClient(t)
	addressBook(contacts)
	ctx := context.Background()
	unnamed := types.NewJID("120363000000000002", types.GroupServer)
	if err := c.store.Save(ctx, []store.Chat{{JID: testGroup.String()}, {JID: unnamed.String()}}, nil); err != nil {
		t.Fatal(err)
	}

	fake.joinedErr = errors.New("offline")
	c.reconcile(ctx, true)
	if got := chatNames(t, c, testGroup)[testGroup.String()]; got != "" {
		t.Errorf("group named %q although listing groups failed", got)
	}

	fake.joinedErr = nil
	fake.joined = []*types.GroupInfo{
		{JID: testGroup, GroupName: types.GroupName{Name: "Family"}},
		{JID: unnamed, Participants: []types.GroupParticipant{
			{JID: aliceLID, PhoneNumber: alicePN},
			{JID: ownJID.ToNonAD()},                            // the account itself is left out
			{JID: types.NewJID("555", types.HiddenUserServer)}, // counted, but has no name
		}},
	}
	c.reconcile(ctx, true)
	want := map[string]string{testGroup.String(): "Family", unnamed.String(): "Alice Saved +1"}
	if got := chatNames(t, c, testGroup, unnamed); !reflect.DeepEqual(got, want) {
		t.Errorf("group names = %v, want %v", got, want)
	}
}
