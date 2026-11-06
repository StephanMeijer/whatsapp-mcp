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

var groupAdminTools = []string{"create_group", "leave_group"}

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
