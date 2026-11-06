// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// MemberResult is what happened to one person in a group change.
type MemberResult struct {
	JID   types.JID
	OK    bool
	Error string // why not, when !OK
}

// GroupCreated is a group made by CreateGroup and how each invited member fared.
type GroupCreated struct {
	JID     types.JID
	Name    string
	Members []MemberResult
}

// CreateGroup makes a group with the account as its admin and tries to add
// members. It fails only when no group was made: once WhatsApp has created
// it, members it refused are reported per member, since retrying would make
// a second group. The group is archived as a chat straight away.
func (c *Client) CreateGroup(ctx context.Context, name string, members []types.JID) (GroupCreated, error) {
	if !c.cli.IsLoggedIn() {
		return GroupCreated{}, errNotLoggedIn
	}
	info, err := c.cli.CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: name, Participants: members})
	if info == nil || info.JID.User == "" {
		if err == nil {
			err = errors.New("WhatsApp returned no group")
		}
		return GroupCreated{}, fmt.Errorf("create group: %w", err)
	}
	if err != nil {
		// whatsmeow returns the group along with errors parsing its attributes.
		c.log.Warn("parse created group", "err", err, "group", info.JID.String())
	}
	group := info.JID.ToNonAD()
	out := GroupCreated{JID: group, Name: info.Name, Members: make([]MemberResult, len(members))}
	for i, m := range members {
		out.Members[i] = c.memberResult(ctx, m, info.Participants)
	}
	if err := c.store.Save(ctx, []store.Chat{{JID: group.String(), Name: info.Name}}, nil); err != nil {
		c.log.Error("archive created group", "err", err, "group", group.String())
	}
	c.log.Info("created group", "group", group.String(), "members", len(members))
	return out, nil
}

// LeaveGroup leaves a group. The archive keeps it, as it does other groups
// the account is no longer in.
func (c *Client) LeaveGroup(ctx context.Context, group types.JID) error {
	if !c.cli.IsLoggedIn() {
		return errNotLoggedIn
	}
	if err := c.cli.LeaveGroup(ctx, group); err != nil {
		return fmt.Errorf("leave group: %w", err)
	}
	c.log.Info("left group", "group", group.String())
	return nil
}

// findParticipant returns the participant that is member, comparing the
// canonical forms of its JID, phone number and LID, or nil.
func (c *Client) findParticipant(ctx context.Context, member types.JID, ps []types.GroupParticipant) *types.GroupParticipant {
	for i, p := range ps {
		for _, j := range []types.JID{p.JID, p.PhoneNumber, p.LID} {
			if !j.IsEmpty() && c.Canonical(ctx, j) == member {
				return &ps[i]
			}
		}
	}
	return nil
}

// memberResult is how adding member went, from the participants WhatsApp
// returned for the change.
func (c *Client) memberResult(ctx context.Context, member types.JID, ps []types.GroupParticipant) MemberResult {
	p := c.findParticipant(ctx, member, ps)
	switch {
	case p == nil:
		return MemberResult{JID: member, Error: "WhatsApp did not report this member"}
	case p.Error != 0:
		return MemberResult{JID: member, Error: participantError(p)}
	}
	return MemberResult{JID: member, OK: true}
}

// participantError explains a participant error code from WhatsApp.
func participantError(p *types.GroupParticipant) string {
	switch {
	case p.Error == 403 && p.AddRequest != nil:
		return "their privacy settings only allow being invited, not added"
	case p.Error == 408:
		return "they left this group recently and can't be added back yet"
	case p.Error == 409:
		return "already a member"
	}
	return fmt.Sprintf("WhatsApp refused (code %d)", p.Error)
}

// UpdateGroupMembers adds, removes, promotes or demotes people in a group,
// reporting each one's outcome. People already in the group are addressed
// by the JID the group itself uses for them, so groups that address members
// by LID work too; someone who isn't a member is reported, not sent.
func (c *Client) UpdateGroupMembers(ctx context.Context, group types.JID, action string, members []types.JID) ([]MemberResult, error) {
	change, ok := map[string]whatsmeow.ParticipantChange{
		"add": whatsmeow.ParticipantChangeAdd, "remove": whatsmeow.ParticipantChangeRemove,
		"promote": whatsmeow.ParticipantChangePromote, "demote": whatsmeow.ParticipantChangeDemote,
	}[action]
	if !ok {
		return nil, fmt.Errorf("action: want add, remove, promote or demote, not %q", action)
	}
	if !c.cli.IsLoggedIn() {
		return nil, errNotLoggedIn
	}
	results := make([]MemberResult, len(members))
	wire := make([]types.JID, 0, len(members))
	sentAs := make(map[int]bool, len(members))
	if change == whatsmeow.ParticipantChangeAdd {
		wire = append(wire, members...)
		for i := range members {
			sentAs[i] = true
		}
	} else {
		info, err := c.cli.GetGroupInfo(ctx, group)
		if err != nil {
			return nil, fmt.Errorf("look up group members: %w", err)
		}
		for i, m := range members {
			p := c.findParticipant(ctx, m, info.Participants)
			if p == nil {
				results[i] = MemberResult{JID: m, Error: "not a member of this group"}
				continue
			}
			wire = append(wire, p.JID)
			sentAs[i] = true
		}
	}
	if len(wire) > 0 {
		changed, err := c.cli.UpdateGroupParticipants(ctx, group, wire, change)
		if err != nil {
			return nil, fmt.Errorf("%s group members: %w", action, err)
		}
		for i, m := range members {
			if sentAs[i] {
				results[i] = c.memberResult(ctx, m, changed)
			}
		}
	}
	c.log.Info("changed group members", "group", group.String(), "action", action, "members", len(members))
	return results, nil
}
