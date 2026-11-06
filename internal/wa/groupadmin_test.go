// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
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

func TestUpdateGroupMembers(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	carol := types.NewJID("31600000003", types.DefaultUserServer)
	// The group addresses Alice by her LID; Carol isn't in it.
	fake.groups[testGroup] = &types.GroupInfo{JID: testGroup, Participants: []types.GroupParticipant{
		{JID: aliceLID, LID: aliceLID, PhoneNumber: alicePN},
		{JID: bobPN, PhoneNumber: bobPN},
	}}

	res, err := c.UpdateGroupMembers(ctx, testGroup, "promote", []types.JID{alicePN, carol})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.memberChanges) != 1 || !slices.Equal(fake.memberChanges[0].members, []types.JID{aliceLID}) ||
		fake.memberChanges[0].action != whatsmeow.ParticipantChangePromote {
		t.Errorf("changes = %+v, want Alice promoted by the LID the group uses", fake.memberChanges)
	}
	want := []MemberResult{{JID: alicePN, OK: true}, {JID: carol, Error: "not a member of this group"}}
	if !slices.Equal(res, want) {
		t.Errorf("results = %+v, want %+v", res, want)
	}

	// add sends the JIDs as given, without looking the group up.
	calls := len(fake.groupInfoCalls)
	fake.memberCodes = map[types.JID]int{carol: 409}
	res, err = c.UpdateGroupMembers(ctx, testGroup, "add", []types.JID{carol})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.groupInfoCalls) != calls || !slices.Equal(fake.memberChanges[1].members, []types.JID{carol}) {
		t.Errorf("add looked the group up or sent %v", fake.memberChanges[1].members)
	}
	if !slices.Equal(res, []MemberResult{{JID: carol, Error: "already a member"}}) {
		t.Errorf("add results = %+v", res)
	}

	// Nobody in the group to change: nothing is sent.
	if _, err := c.UpdateGroupMembers(ctx, testGroup, "remove", []types.JID{carol}); err != nil {
		t.Fatal(err)
	}
	if len(fake.memberChanges) != 2 {
		t.Errorf("a remove of a non-member was sent: %+v", fake.memberChanges[2:])
	}
	if _, err := c.UpdateGroupMembers(ctx, testGroup, "kick", []types.JID{carol}); err == nil {
		t.Error("an unknown action was accepted")
	}
}

func TestSetGroupInfo(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	archive(t, c, incoming(testGroup, alicePN, "G1", 100, "hi"))
	name, desc := "Lunch club", ""
	done, err := c.SetGroupInfo(ctx, testGroup, &name, &desc)
	if err != nil || !done.Name || !done.Description {
		t.Fatalf("SetGroupInfo = %+v, %v", done, err)
	}
	if want := []string{"name " + testGroup.String() + " Lunch club", "topic " + testGroup.String() + " "}; !slices.Equal(fake.groupEdits, want) {
		t.Errorf("edits = %q, want %q", fake.groupEdits, want)
	}
	if names := chatNames(t, c, testGroup); names[testGroup.String()] != "Lunch club" {
		t.Errorf("archived name = %q, want it renamed straight away", names[testGroup.String()])
	}

	fake.topicErr = errors.New("406 not acceptable")
	done, err = c.SetGroupInfo(ctx, testGroup, &name, &desc)
	if err == nil || !done.Name || done.Description {
		t.Errorf("description refused = %+v, %v; want the name reported as applied", done, err)
	}
}

func TestGroupInviteLink(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	var logs bytes.Buffer
	c.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	link, err := c.GroupInviteLink(context.Background(), testGroup, true)
	if err != nil || !strings.HasSuffix(link, "SECRETCODE") {
		t.Fatalf("GroupInviteLink = %q, %v", link, err)
	}
	if strings.Contains(logs.String(), "SECRETCODE") {
		t.Errorf("the invite link was logged: %s", logs.String())
	}
	fake.linkErr = whatsmeow.ErrGroupInviteLinkUnauthorized
	if _, err := c.GroupInviteLink(context.Background(), testGroup, false); err == nil || !strings.Contains(err.Error(), "only an admin") {
		t.Errorf("unauthorized: err = %v, want it in plain words", err)
	}
}

func TestGroupErrorsInPlainWords(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	name := "Lunch club"
	for werr, want := range map[error]string{
		whatsmeow.ErrGroupInviteLinkUnauthorized: "only an admin of this group can do that",
		whatsmeow.ErrNotInGroup:                  "you're not in this group",
		whatsmeow.ErrGroupNotFound:               "this group doesn't exist",
		whatsmeow.ErrIQNotAuthorized:             "only an admin of this group can do that",
		whatsmeow.ErrIQForbidden:                 "you're not in this group, or not allowed",
		whatsmeow.ErrIQNotFound:                  "this group doesn't exist",
		whatsmeow.ErrIQNotAcceptable:             "WhatsApp refused the value",
	} {
		fake.linkErr, fake.nameErr, fake.memberErr = werr, werr, werr
		if _, err := c.GroupInviteLink(ctx, testGroup, false); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("invite link, %v: err = %v, want %q", werr, err, want)
		}
		if _, err := c.SetGroupInfo(ctx, testGroup, &name, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("rename, %v: err = %v, want %q", werr, err, want)
		}
		if _, err := c.UpdateGroupMembers(ctx, testGroup, "add", []types.JID{alicePN}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("add member, %v: err = %v, want %q", werr, err, want)
		}
	}
	if err := groupError(errors.New("timed out")); err.Error() != "timed out" {
		t.Errorf("an unknown error was changed: %v", err)
	}
}
