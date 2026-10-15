// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"sync"
	"sync/atomic"
	"testing"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestGroupNames(t *testing.T) {
	named := types.NewJID("120363000000000001", types.GroupServer)
	unnamed := types.NewJID("120363000000000002", types.GroupServer)
	got := groupNames([]*types.GroupInfo{
		{JID: named, GroupName: types.GroupName{Name: "Family"}},
		{JID: unnamed},
		nil,
	})
	if len(got) != 1 || got[named.String()] != "Family" {
		t.Errorf("groupNames = %v, want only Family", got)
	}
}

func TestGroupNamesCommunities(t *testing.T) {
	community := types.NewJID("120363000000000010", types.GroupServer)
	announcements := types.NewJID("120363000000000011", types.GroupServer)
	namedSub := types.NewJID("120363000000000012", types.GroupServer)
	unnamedSub := types.NewJID("120363000000000013", types.GroupServer)
	orphan := types.NewJID("120363000000000014", types.GroupServer)
	gone := types.NewJID("120363000000000099", types.GroupServer)

	got := groupNames([]*types.GroupInfo{
		{JID: announcements, GroupLinkedParent: types.GroupLinkedParent{LinkedParentJID: community},
			GroupIsDefaultSub: types.GroupIsDefaultSub{IsDefaultSubGroup: true}},
		{JID: community, GroupName: types.GroupName{Name: "Neighbourhood"}, GroupParent: types.GroupParent{IsParent: true}},
		{JID: namedSub, GroupName: types.GroupName{Name: "Garden club"}, GroupLinkedParent: types.GroupLinkedParent{LinkedParentJID: community}},
		{JID: unnamedSub, GroupLinkedParent: types.GroupLinkedParent{LinkedParentJID: community}},
		{JID: orphan, GroupLinkedParent: types.GroupLinkedParent{LinkedParentJID: gone}},
	})
	want := map[string]string{
		community.String():     "Neighbourhood",
		announcements.String(): "Neighbourhood (announcements)",
		namedSub.String():      "Garden club", // own name wins
		unnamedSub.String():    "Neighbourhood",
	}
	if len(got) != len(want) {
		t.Fatalf("groupNames = %v, want %v", got, want)
	}
	for jid, name := range want {
		if got[jid] != name {
			t.Errorf("%s = %q, want %q", jid, got[jid], name)
		}
	}
}

func TestRenameFromGroupInfo(t *testing.T) {
	group := types.NewJID("120363000000000001", types.GroupServer)
	jid, name, ok := renameFromGroupInfo(&events.GroupInfo{JID: group, Name: &types.GroupName{Name: "New name"}})
	if !ok || jid != group.String() || name != "New name" {
		t.Errorf("rename: %q %q %v", jid, name, ok)
	}
	for _, e := range []*events.GroupInfo{
		{JID: group, Topic: &types.GroupTopic{Topic: "only the description changed"}},
		{JID: group, Join: []types.JID{types.NewJID("31600000001", types.DefaultUserServer)}},
		{JID: group, Name: &types.GroupName{}},
		nil,
	} {
		if _, _, ok := renameFromGroupInfo(e); ok {
			t.Errorf("not a rename, but ok: %+v", e)
		}
	}
}

func TestGroupLookupsOncePerGroup(t *testing.T) {
	var g groupLookups
	if !g.shouldLookup("a") || g.shouldLookup("a") {
		t.Error("want true once, then false")
	}
	if !g.shouldLookup("b") {
		t.Error("other groups are independent")
	}

	var c groupLookups
	var trues atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.shouldLookup("same") {
				trues.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := trues.Load(); n != 1 {
		t.Errorf("concurrent lookups allowed %d times, want 1", n)
	}
}

func TestMemberGroupName(t *testing.T) {
	member := func(user string) types.GroupParticipant {
		return types.GroupParticipant{JID: types.NewJID(user, types.DefaultUserServer)}
	}
	names := map[string]string{"1": "Bob", "2": "Alice", "3": "Carol", "4": "Dave", "5": "Erin", "6": "Frank"}
	nameOf := func(p types.GroupParticipant) (string, bool) {
		switch p.JID.User {
		case "0":
			return "", false // the account itself
		case "9":
			return "", true // nameless member
		}
		return names[p.JID.User], true
	}
	tests := []struct {
		name    string
		members []string
		want    string
	}{
		{"sorted, self left out", []string{"0", "1", "2"}, "Alice, Bob"},
		{"nameless members counted", []string{"0", "1", "9"}, "Bob +1"},
		{"capped at five", []string{"0", "1", "2", "3", "4", "5", "6"}, "Alice, Bob, Carol, Dave, Erin +1"},
		{"nobody nameable", []string{"0", "9"}, ""},
		{"only self", []string{"0"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ps []types.GroupParticipant
			for _, u := range tt.members {
				ps = append(ps, member(u))
			}
			if got := memberGroupName(ps, nameOf); got != tt.want {
				t.Errorf("memberGroupName = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParticipantJIDPrefersPhoneNumber(t *testing.T) {
	lid := types.NewJID("111", types.HiddenUserServer)
	pn := types.NewJID("31600000001", types.DefaultUserServer)
	if got := participantJID(types.GroupParticipant{JID: lid, PhoneNumber: pn, LID: lid}); got != pn {
		t.Errorf("with phone number: %s, want %s", got, pn)
	}
	if got := participantJID(types.GroupParticipant{JID: lid, LID: lid}); got != lid {
		t.Errorf("LID only: %s, want %s", got, lid)
	}
}
