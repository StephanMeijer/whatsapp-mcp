// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

const (
	gaAlice    = "31600000001@s.whatsapp.net"
	gaAliceLID = "111@lid"
	gaCarol    = "31600000003@s.whatsapp.net"
	gaListed   = "120363000000000001@g.us"
	gaUnlisted = "120363000000000002@g.us"
	gaSelf     = "31600000000@s.whatsapp.net"
)

var groupAdminTools = []string{"create_group", "leave_group", "update_group_members", "set_group_info", "group_invite_link"}

func TestGroupAdminToolsNeedTheFlag(t *testing.T) {
	has := func(names []string) []string {
		var out []string
		for _, n := range groupAdminTools {
			if slices.Contains(names, n) {
				out = append(out, n)
			}
		}
		return out
	}
	if got := has(toolNames(t, Options{})); len(got) != 0 {
		t.Errorf("without the flag: %v offered", got)
	}
	if got := has(toolNames(t, Options{GroupAdmin: true})); !slices.Equal(got, groupAdminTools) {
		t.Errorf("with the flag: %v offered, want %v", got, groupAdminTools)
	}
	if got := has(toolNames(t, Options{GroupAdmin: true, ReadOnly: true})); len(got) != 0 {
		t.Errorf("read-only with the flag: %v offered", got)
	}
}

func errText(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func TestCreateGroup(t *testing.T) {
	fake := &fakeWA{lids: map[string]string{gaAliceLID: gaAlice}, refuseMembers: map[string]string{}}
	cs := connect(t, fake, storetest.New(t), Options{GroupAdmin: true, AllowRecipients: []string{gaAlice, gaListed}})

	// Alice by phone number and again by LID: one member, allowed.
	var out CreateGroupOut
	if res := call(t, cs, "create_group", map[string]any{"name": " Lunch club ", "participants": []string{"+31 6 0000 0001", gaAliceLID}}, &out); res.IsError {
		t.Fatalf("create_group: %s", errText(res))
	}
	if out.Name != "Lunch club" || len(out.Members) != 1 || !out.Members[0].Added || out.Members[0].JID != gaAlice {
		t.Errorf("create_group = %+v", out)
	}
	if !strings.Contains(out.Note, "add "+out.JID+" to the allowed recipients") {
		t.Errorf("note = %q, want the allowlist hint", out.Note)
	}

	// Refused members don't make it an error: the group exists, and retrying would make another.
	fake.refuseMembers[gaAlice] = "their privacy settings only allow being invited, not added"
	out = CreateGroupOut{}
	if res := call(t, cs, "create_group", map[string]any{"name": "Second", "participants": []string{gaAlice}}, &out); res.IsError {
		t.Fatalf("create_group with a refused member: %s", errText(res))
	}
	if out.JID == "" || out.Members[0].Added || out.Members[0].Error == "" || !strings.Contains(out.Note, "1 of 1 people could not be added") {
		t.Errorf("create_group with a refused member = %+v", out)
	}

	sentBefore := len(fake.sent)
	many := make([]string, 21)
	for i := range many {
		many[i] = fmt.Sprintf("+316100000%02d", i)
	}
	for name, args := range map[string]map[string]any{
		"unlisted member":  {"name": "X", "participants": []string{gaAlice, gaCarol}},
		"yourself":         {"name": "X", "participants": []string{gaSelf}},
		"a group":          {"name": "X", "participants": []string{gaListed}},
		"no members":       {"name": "X", "participants": []string{}},
		"blank name":       {"name": "  ", "participants": []string{gaAlice}},
		"too long a name":  {"name": strings.Repeat("é", maxGroupNameRunes+1), "participants": []string{gaAlice}},
		"too many members": {"name": "X", "participants": many},
	} {
		if res := call(t, cs, "create_group", args, nil); !res.IsError {
			t.Errorf("%s: create_group succeeded", name)
		}
	}
	if len(fake.sent) != sentBefore {
		t.Errorf("refused calls sent %v", fake.sent[sentBefore:])
	}
}

func TestCreateGroupWithoutAllowlist(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{GroupAdmin: true})
	var out CreateGroupOut
	if res := call(t, cs, "create_group", map[string]any{"name": "Open", "participants": []string{gaCarol}}, &out); res.IsError {
		t.Fatalf("create_group: %s", errText(res))
	}
	if out.Note != "" {
		t.Errorf("note = %q, want none without an allowlist and with everyone added", out.Note)
	}
}

func TestLeaveGroup(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{GroupAdmin: true, AllowRecipients: []string{gaListed}})
	var out LeaveGroupOut
	if res := call(t, cs, "leave_group", map[string]any{"chat_jid": gaListed}, &out); res.IsError || !out.Left {
		t.Fatalf("leave_group = %+v, %s", out, errText(res))
	}
	for name, chat := range map[string]string{"unlisted group": gaUnlisted, "a person": gaAlice, "missing": ""} {
		if res := call(t, cs, "leave_group", map[string]any{"chat_jid": chat}, nil); !res.IsError {
			t.Errorf("%s: leave_group succeeded", name)
		}
	}
	if want := []string{"leave " + gaListed}; !slices.Equal(fake.sent, want) {
		t.Errorf("sent = %v, want %v", fake.sent, want)
	}
}

func TestGroupToolsRefuseAPerson(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{GroupAdmin: true})
	res := call(t, cs, "leave_group", map[string]any{"chat_jid": gaAlice}, nil)
	if !res.IsError || !strings.Contains(errText(res), "is not a group") {
		t.Errorf("leave_group on a person = %q, want 'is not a group'", errText(res))
	}
	if len(fake.sent) != 0 {
		t.Errorf("sent = %v", fake.sent)
	}
}

func TestUpdateGroupMembers(t *testing.T) {
	fake := &fakeWA{lids: map[string]string{gaAliceLID: gaAlice}, refuseMembers: map[string]string{}}
	cs := connect(t, fake, storetest.New(t), Options{GroupAdmin: true, AllowRecipients: []string{gaAlice, gaListed}})
	update := func(action string, members ...string) *mcp.CallToolResult {
		t.Helper()
		return call(t, cs, "update_group_members", map[string]any{"chat_jid": gaListed, "action": action, "members": members}, nil)
	}

	// add and promote: the people must be allowed; remove and demote: anyone already there.
	for _, action := range []string{"add", "promote"} {
		if res := update(action, gaAliceLID); res.IsError {
			t.Errorf("%s an allowed member: %s", action, errText(res))
		}
		if res := update(action, gaCarol); !res.IsError {
			t.Errorf("%s an unlisted member succeeded", action)
		}
	}
	for _, action := range []string{"remove", "demote"} {
		if res := update(action, gaCarol); res.IsError {
			t.Errorf("%s an unlisted member: %s", action, errText(res))
		}
	}
	want := []string{
		"add " + gaListed + " [" + gaAlice + "]",
		"promote " + gaListed + " [" + gaAlice + "]",
		"remove " + gaListed + " [" + gaCarol + "]",
		"demote " + gaListed + " [" + gaCarol + "]",
	}
	if !slices.Equal(fake.sent, want) {
		t.Errorf("sent = %v, want %v", fake.sent, want)
	}

	// One refused of two is a success listing both; all refused is an error.
	fake.refuseMembers[gaCarol] = "not a member of this group"
	var out UpdateGroupMembersOut
	if res := call(t, cs, "update_group_members", map[string]any{"chat_jid": gaListed, "action": "remove", "members": []string{gaAlice, gaCarol}}, &out); res.IsError {
		t.Fatalf("partly refused: %s", errText(res))
	}
	if len(out.Members) != 2 || !out.Members[0].OK || out.Members[1].OK || out.Members[1].Error == "" {
		t.Errorf("partly refused = %+v", out)
	}
	if res := update("remove", gaCarol); !res.IsError || !strings.Contains(errText(res), "nobody could be changed") {
		t.Errorf("all refused = %q, want an error", errText(res))
	}

	sentBefore := len(fake.sent)
	for name, args := range map[string]map[string]any{
		"unknown action": {"chat_jid": gaListed, "action": "kick", "members": []string{gaAlice}},
		"unlisted group": {"chat_jid": gaUnlisted, "action": "remove", "members": []string{gaAlice}},
		"no members":     {"chat_jid": gaListed, "action": "remove", "members": []string{}},
	} {
		if res := call(t, cs, "update_group_members", args, nil); !res.IsError {
			t.Errorf("%s: update_group_members succeeded", name)
		}
	}
	if len(fake.sent) != sentBefore {
		t.Errorf("refused calls sent %v", fake.sent[sentBefore:])
	}
}

func TestSetGroupInfo(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{GroupAdmin: true, AllowRecipients: []string{gaListed}})
	set := func(args map[string]any) (SetGroupInfoOut, *mcp.CallToolResult) {
		t.Helper()
		var out SetGroupInfoOut
		res := call(t, cs, "set_group_info", args, &out)
		return out, res
	}
	if out, res := set(map[string]any{"chat_jid": gaListed, "name": " Lunch club "}); res.IsError || !out.NameChanged || out.DescriptionChanged {
		t.Errorf("rename = %+v, %s", out, errText(res))
	}
	// An empty description is sent, to clear it, not dropped.
	if out, res := set(map[string]any{"chat_jid": gaListed, "description": ""}); res.IsError || !out.DescriptionChanged {
		t.Errorf("clear description = %+v, %s", out, errText(res))
	}
	if want := []string{`name ` + gaListed + ` "Lunch club"`, `description ` + gaListed + ` ""`}; !slices.Equal(fake.sent, want) {
		t.Errorf("sent = %v, want %v", fake.sent, want)
	}

	sentBefore := len(fake.sent)
	for name, args := range map[string]map[string]any{
		"neither field":        {"chat_jid": gaListed},
		"blank name":           {"chat_jid": gaListed, "name": "   "},
		"too long description": {"chat_jid": gaListed, "description": strings.Repeat("é", maxGroupDescriptionRunes+1)},
		"unlisted group":       {"chat_jid": gaUnlisted, "name": "X"},
	} {
		if _, res := set(args); !res.IsError {
			t.Errorf("%s: set_group_info succeeded", name)
		}
	}
	if len(fake.sent) != sentBefore {
		t.Errorf("refused calls sent %v", fake.sent[sentBefore:])
	}

	// A failure after the rename says the rename happened.
	fake.descriptionErr = fmt.Errorf("406 not acceptable")
	_, res := set(map[string]any{"chat_jid": gaListed, "name": "New", "description": "too much"})
	if !res.IsError || !strings.Contains(errText(res), "the name was changed, but not the description") {
		t.Errorf("partial failure = %q", errText(res))
	}
}

func TestGroupInviteLink(t *testing.T) {
	link := func(opts Options, chat string, reset bool) (GroupInviteLinkOut, *mcp.CallToolResult, *fakeWA) {
		t.Helper()
		fake := &fakeWA{}
		cs := connect(t, fake, storetest.New(t), opts)
		var out GroupInviteLinkOut
		res := call(t, cs, "group_invite_link", map[string]any{"chat_jid": chat, "reset": reset}, &out)
		return out, res, fake
	}
	allowlisted := Options{GroupAdmin: true, AllowRecipients: []string{gaListed}}

	// With an allowlist and the flag off, links are refused, reset too, and nothing is sent.
	for _, reset := range []bool{false, true} {
		_, res, fake := link(allowlisted, gaListed, reset)
		if !res.IsError || !strings.Contains(errText(res), "-allow-invite-links") || len(fake.sent) != 0 {
			t.Errorf("allowlist, flag off, reset=%v: %q, sent %v", reset, errText(res), fake.sent)
		}
	}
	// With the flag on, a listed group gets its link; an unlisted one is still refused.
	withFlag := allowlisted
	withFlag.InviteLinks = true
	if out, res, _ := link(withFlag, gaListed, true); res.IsError || out.Link == "" || !out.Reset {
		t.Errorf("allowlist, flag on = %+v, %s", out, errText(res))
	}
	if _, res, _ := link(withFlag, gaUnlisted, false); !res.IsError {
		t.Error("allowlist, flag on: an unlisted group got a link")
	}
	// Without an allowlist, links always work.
	if out, res, _ := link(Options{GroupAdmin: true}, gaUnlisted, false); res.IsError || out.Link == "" {
		t.Errorf("no allowlist = %+v, %s", out, errText(res))
	}
}
