// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxGroupNameRunes only stops absurd input; WhatsApp's own, shorter limit
// is enforced by WhatsApp, whose refusal is passed on.
const maxGroupNameRunes = 256

// registerGroupAdmin adds the tools that create, change and leave groups.
// They are hard to undo, so the operator has to switch them on.
func registerGroupAdmin(s *mcp.Server, t *tools) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "create_group",
		Description: "Create a group with you as its admin and add people to it. Calling this again creates " +
			"another group, so check the result before retrying: people WhatsApp refused are listed per member.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true)},
	}, t.createGroup)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "leave_group",
		Description: "Leave a group. You can only get back in if an admin adds you or you have an invite link.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true), DestructiveHint: new(true)},
	}, t.leaveGroup)
	mcp.AddTool(s, &mcp.Tool{
		Name: "update_group_members",
		Description: "Add, remove, promote to admin or demote people in a group you admin, up to 20 per call. " +
			"Each person's outcome is listed; the call fails only when nobody could be changed.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(true), DestructiveHint: new(true)},
	}, t.updateGroupMembers)
}

type CreateGroupIn struct {
	Name         string   `json:"name" jsonschema:"the group's name"`
	Participants []string `json:"participants" jsonschema:"JIDs or international phone numbers of the people to add, at most 20"`
}

type GroupMember struct {
	JID   string `json:"jid"`
	Name  string `json:"name,omitempty"`
	Added bool   `json:"added"`
	Error string `json:"error,omitempty" jsonschema:"why WhatsApp refused this person"`
}

type CreateGroupOut struct {
	JID     string        `json:"jid" jsonschema:"the new group's JID"`
	Name    string        `json:"name"`
	Members []GroupMember `json:"members"`
	Note    string        `json:"note,omitempty"`
}

func (t *tools) createGroup(ctx context.Context, _ *mcp.CallToolRequest, in CreateGroupIn) (*mcp.CallToolResult, CreateGroupOut, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, CreateGroupOut{}, errors.New("the group's name is required")
	}
	if utf8.RuneCountInString(name) > maxGroupNameRunes {
		return nil, CreateGroupOut{}, fmt.Errorf("the name is over %d characters", maxGroupNameRunes)
	}
	members, err := t.policy.groupMembers(ctx, t.wa, in.Participants, true)
	if err != nil {
		return nil, CreateGroupOut{}, err
	}
	g, err := t.wa.CreateGroup(ctx, name, members)
	if err != nil {
		return nil, CreateGroupOut{}, err
	}
	jids := make([]string, len(g.Members))
	for i, m := range g.Members {
		jids[i] = m.JID.String()
	}
	names := t.senderNames(ctx, jids)
	out := CreateGroupOut{JID: g.JID.String(), Name: g.Name, Members: make([]GroupMember, len(g.Members))}
	var notes []string
	refused := 0
	for i, m := range g.Members {
		out.Members[i] = GroupMember{JID: jids[i], Name: names[jids[i]], Added: m.OK, Error: m.Error}
		if !m.OK {
			refused++
		}
	}
	if refused > 0 {
		notes = append(notes, fmt.Sprintf("The group was created, but %d of %d people could not be added; see members.", refused, len(g.Members)))
	}
	if t.policy.hasAllowlist() {
		notes = append(notes, "To send to this group or change it here, add "+out.JID+" to the allowed recipients and restart the server.")
	}
	out.Note = strings.Join(notes, " ")
	return nil, out, nil
}

type LeaveGroupIn struct {
	ChatJID string `json:"chat_jid" jsonschema:"the group's JID"`
}

type LeaveGroupOut struct {
	JID  string `json:"jid"`
	Left bool   `json:"left"`
}

func (t *tools) leaveGroup(ctx context.Context, _ *mcp.CallToolRequest, in LeaveGroupIn) (*mcp.CallToolResult, LeaveGroupOut, error) {
	group, err := t.policy.groupChat(ctx, t.wa, in.ChatJID)
	if err != nil {
		return nil, LeaveGroupOut{}, err
	}
	if err := t.wa.LeaveGroup(ctx, group); err != nil {
		return nil, LeaveGroupOut{}, err
	}
	return nil, LeaveGroupOut{JID: group.String(), Left: true}, nil
}

type UpdateGroupMembersIn struct {
	ChatJID string   `json:"chat_jid" jsonschema:"the group's JID"`
	Action  string   `json:"action" jsonschema:"add, remove, promote or demote"`
	Members []string `json:"members" jsonschema:"JIDs or international phone numbers, at most 20"`
}

type UpdatedMember struct {
	JID   string `json:"jid"`
	Name  string `json:"name,omitempty"`
	OK    bool   `json:"ok" jsonschema:"true when this person's change went through"`
	Error string `json:"error,omitempty" jsonschema:"why WhatsApp refused this person"`
}

type UpdateGroupMembersOut struct {
	JID     string          `json:"jid"`
	Action  string          `json:"action"`
	Members []UpdatedMember `json:"members"`
}

// checkedActions bring someone new in or give them power over the group,
// so the people they name must pass the allowlist; remove and demote act
// on people already there.
var checkedActions = map[string]bool{"add": true, "promote": true}

func (t *tools) updateGroupMembers(ctx context.Context, _ *mcp.CallToolRequest, in UpdateGroupMembersIn) (*mcp.CallToolResult, UpdateGroupMembersOut, error) {
	switch in.Action {
	case "add", "remove", "promote", "demote":
	default:
		return nil, UpdateGroupMembersOut{}, fmt.Errorf("action: want add, remove, promote or demote, not %q", in.Action)
	}
	group, err := t.policy.groupChat(ctx, t.wa, in.ChatJID)
	if err != nil {
		return nil, UpdateGroupMembersOut{}, err
	}
	members, err := t.policy.groupMembers(ctx, t.wa, in.Members, checkedActions[in.Action])
	if err != nil {
		return nil, UpdateGroupMembersOut{}, err
	}
	results, err := t.wa.UpdateGroupMembers(ctx, group, in.Action, members)
	if err != nil {
		return nil, UpdateGroupMembersOut{}, err
	}
	jids := make([]string, len(results))
	for i, r := range results {
		jids[i] = r.JID.String()
	}
	names := t.senderNames(ctx, jids)
	out := UpdateGroupMembersOut{JID: group.String(), Action: in.Action, Members: make([]UpdatedMember, len(results))}
	var reasons []string
	for i, r := range results {
		out.Members[i] = UpdatedMember{JID: jids[i], Name: names[jids[i]], OK: r.OK, Error: r.Error}
		if !r.OK {
			reasons = append(reasons, jids[i]+": "+r.Error)
		}
	}
	if len(reasons) == len(results) {
		return nil, UpdateGroupMembersOut{}, fmt.Errorf("nobody could be changed: %s", strings.Join(reasons, "; "))
	}
	return nil, out, nil
}
