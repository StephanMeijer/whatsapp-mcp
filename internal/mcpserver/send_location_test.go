// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"slices"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

func TestSendLocation(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{})
	var out Message
	call(t, cs, "send_location", map[string]any{
		"recipient": "+31600000001", "latitude": 52.37, "longitude": 4.89, "name": "Dam", "address": "Amsterdam",
	}, &out)
	if out.MediaType != "location" {
		t.Errorf("out = %+v", out)
	}
	if want := []string{`location 31600000001@s.whatsapp.net 52.37,4.89 "Dam" "Amsterdam"`}; !slices.Equal(fake.sent, want) {
		t.Errorf("sent = %q, want %q", fake.sent, want)
	}
}

func TestSendLocationRespectsAllowlistAndReadOnly(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{AllowRecipients: []string{"+31600000001"}})
	if res := call(t, cs, "send_location", map[string]any{"recipient": "+31600000002", "latitude": 1, "longitude": 2}, nil); !res.IsError {
		t.Error("recipient outside the allowlist: want tool error")
	}
	if len(fake.sent) != 0 {
		t.Errorf("refused call still sent: %q", fake.sent)
	}
	if got := toolNames(t, Options{ReadOnly: true}); slices.Contains(got, "send_location") {
		t.Errorf("read-only tools include send_location: %v", got)
	}
}
