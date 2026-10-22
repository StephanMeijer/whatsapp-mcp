// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"reflect"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
)

func TestGroupFromInfo(t *testing.T) {
	pn := func(n string) types.JID { return types.NewJID(n, types.DefaultUserServer) }
	lid := types.NewJID("111", types.HiddenUserServer)
	canon := func(j types.JID) types.JID {
		if j == lid {
			return pn("31600000003")
		}
		return j.ToNonAD()
	}
	names := map[types.JID]string{pn("31600000001"): "Alice", pn("31600000003"): "Carol"}
	info := &types.GroupInfo{
		JID:               types.NewJID("123", types.GroupServer),
		OwnerJID:          pn("31600000001"),
		GroupName:         types.GroupName{Name: "Trip"},
		GroupTopic:        types.GroupTopic{Topic: "Plans for May"},
		GroupLocked:       types.GroupLocked{IsLocked: true},
		GroupAnnounce:     types.GroupAnnounce{IsAnnounce: true},
		GroupLinkedParent: types.GroupLinkedParent{LinkedParentJID: types.NewJID("999", types.GroupServer)},
		GroupCreated:      time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC),
		Participants: []types.GroupParticipant{
			{JID: pn("31600000001"), IsAdmin: true, IsSuperAdmin: true},
			{JID: types.JID{User: "31600000002", Device: 3, Server: types.DefaultUserServer}, IsAdmin: true},
			{JID: lid, LID: lid}, // only known by @lid: canonicalised and named
		},
	}
	want := Group{
		JID:                "123@g.us",
		Name:               "Trip",
		Description:        "Plans for May",
		CreatedAt:          "2024-05-01T12:00:00Z",
		CreatedBy:          "31600000001@s.whatsapp.net",
		Community:          "999@g.us",
		OnlyAdminsSend:     true,
		OnlyAdminsEditInfo: true,
		Members: []GroupMember{
			{JID: "31600000001@s.whatsapp.net", Name: "Alice", Role: "super admin"},
			{JID: "31600000002@s.whatsapp.net", Role: "admin"},
			{JID: "31600000003@s.whatsapp.net", Name: "Carol", Role: "member"},
		},
	}
	if got := groupFromInfo(info, canon, names); !reflect.DeepEqual(got, want) {
		t.Errorf("groupFromInfo =\n %+v\nwant\n %+v", got, want)
	}
}

func TestGroupFromInfoLeavesOutDeletedTopicAndZeroTimes(t *testing.T) {
	info := &types.GroupInfo{
		JID:        types.NewJID("123", types.GroupServer),
		GroupTopic: types.GroupTopic{Topic: "old", TopicDeleted: true},
	}
	want := Group{JID: "123@g.us", Members: []GroupMember{}}
	if got := groupFromInfo(info, func(j types.JID) types.JID { return j }, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("groupFromInfo = %+v, want %+v", got, want)
	}
}
