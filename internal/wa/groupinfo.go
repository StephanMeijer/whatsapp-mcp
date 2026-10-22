// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// GroupMember is one participant of a group.
type GroupMember struct {
	JID  string `json:"jid" jsonschema:"phone-number JID where known"`
	Name string `json:"name,omitempty" jsonschema:"the saved contact name, otherwise their WhatsApp push name"`
	Role string `json:"role" jsonschema:"member, admin or super admin"`
}

// Group is a group's current info as WhatsApp reports it.
type Group struct {
	JID                  string        `json:"jid"`
	Name                 string        `json:"name,omitempty"`
	Description          string        `json:"description,omitempty"`
	CreatedAt            string        `json:"created_at,omitempty" jsonschema:"RFC 3339"`
	CreatedBy            string        `json:"created_by,omitempty" jsonschema:"JID of the creator"`
	Community            string        `json:"community,omitempty" jsonschema:"JID of the community this group belongs to, if any"`
	OnlyAdminsSend       bool          `json:"only_admins_send"`
	OnlyAdminsEditInfo   bool          `json:"only_admins_edit_info"`
	JoinApprovalRequired bool          `json:"join_approval_required"`
	Members              []GroupMember `json:"members"`
}

// GroupInfo asks WhatsApp for a group's info. Members are canonicalised to
// phone-number JIDs where known and named from the address book.
func (c *Client) GroupInfo(ctx context.Context, group types.JID) (Group, error) {
	if !c.cli.IsLoggedIn() {
		return Group{}, errors.New("not logged in to WhatsApp")
	}
	info, err := c.cli.GetGroupInfo(ctx, group.ToNonAD())
	if err != nil {
		return Group{}, fmt.Errorf("group info: %w", err)
	}
	canon := c.memoCanonical(ctx)
	jids := make([]types.JID, 0, len(info.Participants))
	for _, p := range info.Participants {
		jids = append(jids, canon(participantJID(p)))
	}
	names, err := c.Names(ctx, jids)
	if err != nil {
		c.log.Warn("group info: look up member names", "err", err, "group", group.String())
	}
	return groupFromInfo(info, canon, names), nil
}

// groupFromInfo maps whatsmeow's group info to Group.
func groupFromInfo(info *types.GroupInfo, canon func(types.JID) types.JID, names map[types.JID]string) Group {
	g := Group{
		JID:                  info.JID.ToNonAD().String(),
		Name:                 info.Name,
		OnlyAdminsSend:       info.IsAnnounce,
		OnlyAdminsEditInfo:   info.IsLocked,
		JoinApprovalRequired: info.IsJoinApprovalRequired,
		Members:              make([]GroupMember, 0, len(info.Participants)),
	}
	if !info.TopicDeleted {
		g.Description = info.Topic
	}
	if !info.GroupCreated.IsZero() {
		g.CreatedAt = info.GroupCreated.UTC().Format(time.RFC3339)
	}
	if !info.OwnerJID.IsEmpty() {
		g.CreatedBy = canon(info.OwnerJID).String()
	}
	if !info.LinkedParentJID.IsEmpty() {
		g.Community = info.LinkedParentJID.ToNonAD().String()
	}
	for _, p := range info.Participants {
		jid := canon(participantJID(p))
		role := "member"
		switch {
		case p.IsSuperAdmin:
			role = "super admin"
		case p.IsAdmin:
			role = "admin"
		}
		g.Members = append(g.Members, GroupMember{JID: jid.String(), Name: names[jid], Role: role})
	}
	return g
}
