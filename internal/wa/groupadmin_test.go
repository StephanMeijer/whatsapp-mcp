// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"slices"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestCreateGroup(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	group := types.NewJID("120363000000000077", types.GroupServer)
	carol := types.NewJID("31600000003", types.DefaultUserServer)
	fake.created = &types.GroupInfo{
		JID:       group,
		GroupName: types.GroupName{Name: "Lunch club"},
		Participants: []types.GroupParticipant{
			{JID: ownLID.ToNonAD(), IsSuperAdmin: true},
			// Alice is reported by her LID only.
			{JID: aliceLID, LID: aliceLID},
			{JID: carol, PhoneNumber: carol, Error: 403, AddRequest: &types.GroupParticipantAddRequest{Code: "x"}},
		},
	}
	g, err := c.CreateGroup(ctx, "Lunch club", []types.JID{alicePN, carol, bobPN})
	if err != nil {
		t.Fatal(err)
	}
	if g.JID != group || g.Name != "Lunch club" || len(fake.createReqs) != 1 || fake.createReqs[0].Name != "Lunch club" {
		t.Fatalf("CreateGroup = %+v, requests %+v", g, fake.createReqs)
	}
	want := []MemberResult{
		{JID: alicePN, OK: true},
		{JID: carol, Error: "their privacy settings only allow being invited, not added"},
		{JID: bobPN, Error: "WhatsApp did not report this member"},
	}
	if !slices.Equal(g.Members, want) {
		t.Errorf("members = %+v, want %+v", g.Members, want)
	}
	if names := chatNames(t, c, group); names[group.String()] != "Lunch club" {
		t.Errorf("archived chats = %v, want the new group", names)
	}

	// The group exists even when whatsmeow fails to parse part of the reply.
	partial := types.NewJID("120363000000000078", types.GroupServer)
	fake.created = &types.GroupInfo{JID: partial, GroupName: types.GroupName{Name: "Partial"}}
	fake.createErr = errors.New("failed to parse creation")
	if g, err := c.CreateGroup(ctx, "Partial", nil); err != nil || g.JID != partial {
		t.Errorf("created with a parse error = %+v, %v; want the group", g, err)
	}
	if names := chatNames(t, c, partial); names[partial.String()] != "Partial" {
		t.Errorf("archived chats = %v, want the partly parsed group", names)
	}
	fake.created = nil
	fake.createErr = errors.New("406 not acceptable")
	if _, err := c.CreateGroup(ctx, "X", []types.JID{alicePN}); err == nil {
		t.Error("a failed create was not an error")
	}
	fake.loggedIn = false
	if _, err := c.CreateGroup(ctx, "X", []types.JID{alicePN}); !errors.Is(err, errNotLoggedIn) {
		t.Errorf("logged out: err %v", err)
	}
}

func TestLeaveGroup(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	if err := c.LeaveGroup(context.Background(), testGroup); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fake.left, []types.JID{testGroup}) {
		t.Errorf("left = %v", fake.left)
	}
	fake.leaveErr = errors.New("not in group")
	if err := c.LeaveGroup(context.Background(), testGroup); err == nil {
		t.Error("a failed leave was not an error")
	}
}

func TestParticipantError(t *testing.T) {
	for _, tc := range []struct {
		p    types.GroupParticipant
		want string
	}{
		{types.GroupParticipant{Error: 403, AddRequest: &types.GroupParticipantAddRequest{}}, "their privacy settings only allow being invited, not added"},
		{types.GroupParticipant{Error: 408}, "they left this group recently and can't be added back yet"},
		{types.GroupParticipant{Error: 409}, "already a member"},
		{types.GroupParticipant{Error: 403}, "WhatsApp refused (code 403)"},
	} {
		if got := participantError(&tc.p); got != tc.want {
			t.Errorf("participantError(%d) = %q, want %q", tc.p.Error, got, tc.want)
		}
	}
}
