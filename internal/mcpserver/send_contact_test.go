// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"slices"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

func TestSendContact(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{})
	var out Message
	call(t, cs, "send_contact", map[string]any{"recipient": "+31600000001", "name": "Jane", "phone": "+31612345678"}, &out)
	if out.MediaType != "contact" {
		t.Errorf("out = %+v", out)
	}
	if want := []string{`contact 31600000001@s.whatsapp.net "Jane" +31612345678`}; !slices.Equal(fake.sent, want) {
		t.Errorf("sent = %q, want %q", fake.sent, want)
	}
}

func TestSendContactRespectsAllowlistAndReadOnly(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{AllowRecipients: []string{"+31600000001"}})
	if res := call(t, cs, "send_contact", map[string]any{"recipient": "+31600000002", "name": "Jane", "phone": "+31612345678"}, nil); !res.IsError {
		t.Error("recipient outside the allowlist: want tool error")
	}
	if len(fake.sent) != 0 {
		t.Errorf("refused call still sent: %q", fake.sent)
	}
	if got := toolNames(t, Options{ReadOnly: true}); slices.Contains(got, "send_contact") {
		t.Errorf("read-only tools include send_contact: %v", got)
	}
}
