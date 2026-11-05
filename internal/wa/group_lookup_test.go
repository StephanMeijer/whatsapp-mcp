// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"testing"

	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestLookupGroupName(t *testing.T) {
	community := types.NewJID("120363000000000009", types.GroupServer)
	tests := []struct {
		name      string
		archived  string // the group's name in the archive before the lookup
		groups    map[types.JID]*types.GroupInfo
		groupErr  error
		want      string
		wantCalls int
	}{
		{
			name:     "already named: no lookup",
			archived: "Old name",
			groups:   map[types.JID]*types.GroupInfo{testGroup: {JID: testGroup, GroupName: types.GroupName{Name: "New"}}},
			want:     "Old name",
		},
		{
			name:      "named group",
			groups:    map[types.JID]*types.GroupInfo{testGroup: {JID: testGroup, GroupName: types.GroupName{Name: "Climbing"}}},
			want:      "Climbing",
			wantCalls: 1,
		},
		{
			name: "nameless community subgroup takes the community's name",
			groups: map[types.JID]*types.GroupInfo{
				testGroup: {JID: testGroup, GroupLinkedParent: types.GroupLinkedParent{LinkedParentJID: community}},
				community: {JID: community, GroupName: types.GroupName{Name: "Neighbours"}},
			},
			want:      "Neighbours",
			wantCalls: 2,
		},
		{
			name: "nameless group is named after its members, without the account",
			groups: map[types.JID]*types.GroupInfo{testGroup: {JID: testGroup, Participants: []types.GroupParticipant{
				{JID: ownJID.ToNonAD()},
				{JID: aliceLID, PhoneNumber: alicePN},
				{JID: bobPN},
			}}},
			want:      "+31600000002, Alice",
			wantCalls: 1,
		},
		{
			name:      "lookup error leaves the chat as it was",
			groupErr:  errors.New("offline"),
			want:      "",
			wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, fake, _, contacts := newTestClient(t)
			contacts.all[alicePN] = types.ContactInfo{Found: true, FullName: "Alice"}
			fake.groups, fake.groupErr = tt.groups, tt.groupErr
			ctx := context.Background()
			if err := c.store.Save(ctx, []store.Chat{{JID: testGroup.String(), Name: tt.archived}}, nil); err != nil {
				t.Fatal(err)
			}

			c.lookupGroupName(ctx, testGroup)

			if got := chatNames(t, c, testGroup)[testGroup.String()]; got != tt.want {
				t.Errorf("name = %q, want %q", got, tt.want)
			}
			if len(fake.groupInfoCalls) != tt.wantCalls {
				t.Errorf("GetGroupInfo calls = %v, want %d", fake.groupInfoCalls, tt.wantCalls)
			}
		})
	}
}
