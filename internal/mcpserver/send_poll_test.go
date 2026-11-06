// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"slices"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

func TestSendPoll(t *testing.T) {
	const bob, carol = "31600000002@s.whatsapp.net", "31600000003@s.whatsapp.net"
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{AllowRecipients: []string{bob}})

	var out Message
	args := map[string]any{"recipient": "+31 6 0000 0002", "question": "Lunch?", "options": []string{"Pizza", "Sushi"}, "multiple": true}
	if res := call(t, cs, "send_poll", args, &out); res.IsError {
		t.Fatalf("send_poll: %v", res.Content)
	}
	if out.ChatJID != bob || out.MediaType != "poll" || !out.FromMe {
		t.Errorf("send_poll = %+v", out)
	}
	if res := call(t, cs, "send_poll", map[string]any{"recipient": carol, "question": "Q", "options": []string{"A", "B"}}, nil); !res.IsError {
		t.Error("send_poll to a recipient outside the allowlist succeeded")
	}
	if want := []string{`poll ` + bob + ` "Lunch?" ["Pizza" "Sushi"] multiple=true`}; !slices.Equal(fake.sent, want) {
		t.Errorf("sent = %v, want %v", fake.sent, want)
	}
}
