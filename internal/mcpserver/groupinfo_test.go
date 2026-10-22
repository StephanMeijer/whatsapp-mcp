// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

const trip = "123@g.us"

var tripGroup = wa.Group{
	JID:            trip,
	Name:           "Trip",
	Description:    "Plans for May",
	OnlyAdminsSend: true,
	Members: []wa.GroupMember{
		{JID: alice, Name: "Alice", Role: "super admin"},
		{JID: "31600000002@s.whatsapp.net", Role: "member"},
	},
}

func TestGroupInfo(t *testing.T) {
	for _, opts := range []Options{{}, {ReadOnly: true}} {
		fake := &fakeWA{groups: map[string]wa.Group{trip: tripGroup}}
		cs := connect(t, fake, storetest.New(t), opts)
		var out GroupInfoOut
		if res := call(t, cs, "group_info", map[string]any{"chat_jid": trip}, &out); res.IsError {
			t.Fatalf("group_info (read-only=%v): %v", opts.ReadOnly, res.Content)
		}
		if !reflect.DeepEqual(out.Group, tripGroup) {
			t.Errorf("group = %+v, want %+v", out.Group, tripGroup)
		}
	}
}

func TestGroupInfoRejectsNonGroupJIDsWithoutLookup(t *testing.T) {
	for _, chat := range []string{"", alice, "+31600000001", "not a jid"} {
		fake := &fakeWA{groups: map[string]wa.Group{trip: tripGroup}}
		cs := connect(t, fake, storetest.New(t), Options{})
		if res := call(t, cs, "group_info", map[string]any{"chat_jid": chat}, nil); !res.IsError {
			t.Errorf("group_info(%q): want tool error", chat)
		}
		if len(fake.groupCalls) != 0 {
			t.Errorf("group_info(%q): WhatsApp was asked: %v", chat, fake.groupCalls)
		}
	}
}

func TestGroupInfoUnknownGroup(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{})
	res := call(t, cs, "group_info", map[string]any{"chat_jid": "999@g.us"}, nil)
	if !res.IsError {
		t.Fatal("want tool error")
	}
	if got := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(got, "does not exist") {
		t.Errorf("error = %q, want the lookup's reason", got)
	}
}
