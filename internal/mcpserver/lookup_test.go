// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

// phoneNumbers returns n distinct, valid numbers.
func phoneNumbers(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("+316000%04d", i)
	}
	return out
}

func TestCheckNumber(t *testing.T) {
	fake := &fakeWA{registered: map[string]string{"+31600000001": alice}}
	cs := connect(t, fake, storetest.New(t), Options{})

	// Numbers are written differently, one is repeated.
	var out CheckNumberOut
	res := call(t, cs, "check_number", map[string]any{
		"phones": []string{"+31 6 0000 0001", "0031600000002", "+31600000001"},
	}, &out)
	if res.IsError {
		t.Fatalf("check_number: %v", res.Content)
	}
	want := []wa.NumberCheck{
		{Phone: "+31600000001", OnWhatsApp: true, JID: alice},
		{Phone: "+31600000002"},
	}
	if !slices.Equal(out.Numbers, want) {
		t.Errorf("numbers = %+v, want %+v", out.Numbers, want)
	}
	if wantCalls := [][]string{{"+31600000001", "+31600000002"}}; !slices.EqualFunc(fake.checkCalls, wantCalls, slices.Equal) {
		t.Errorf("WhatsApp was asked about %v, want %v", fake.checkCalls, wantCalls)
	}

	// The JID it returns is one send_message accepts.
	if res := call(t, cs, "send_message", map[string]any{"recipient": out.Numbers[0].JID, "text": "hi"}, nil); res.IsError {
		t.Errorf("send_message to the returned JID: %v", res.Content)
	}
	if want := []string{alice + ": hi"}; !slices.Equal(fake.sent, want) {
		t.Errorf("sent = %v, want %v", fake.sent, want)
	}
}

func TestCheckNumberRejectsBadInputWithoutLookup(t *testing.T) {
	tests := []struct {
		name   string
		phones []string
	}{
		{"no numbers", nil},
		{"national number", []string{"0612345678"}},
		{"no country code marker", []string{"31612345678"}},
		{"a JID", []string{alice}},
		{"one bad number among good ones", []string{"+31600000001", "not a number"}},
		{"too many numbers", phoneNumbers(checkNumberMax + 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeWA{}
			cs := connect(t, fake, storetest.New(t), Options{})
			args := map[string]any{"phones": tt.phones}
			if tt.phones == nil {
				args = map[string]any{}
			}
			if res := call(t, cs, "check_number", args, nil); !res.IsError {
				t.Error("want tool error")
			}
			if len(fake.checkCalls) != 0 {
				t.Errorf("WhatsApp was contacted: %v", fake.checkCalls)
			}
		})
	}
}

func TestCheckNumberAtTheBatchLimit(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{})
	var out CheckNumberOut
	if res := call(t, cs, "check_number", map[string]any{"phones": phoneNumbers(checkNumberMax)}, &out); res.IsError {
		t.Fatalf("check_number: %v", res.Content)
	}
	if len(out.Numbers) != checkNumberMax {
		t.Errorf("got %d results, want %d", len(out.Numbers), checkNumberMax)
	}
}

func TestCheckNumberLookupFailure(t *testing.T) {
	fake := &fakeWA{checkErr: errors.New("not logged in to WhatsApp")}
	cs := connect(t, fake, storetest.New(t), Options{})
	res := call(t, cs, "check_number", map[string]any{"phones": []string{"+31600000001"}}, nil)
	if !res.IsError {
		t.Fatal("want tool error")
	}
	if got := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(got, "not logged in") {
		t.Errorf("error = %q, want the lookup's reason", got)
	}
}

func TestCheckNumberIsAvailableReadOnly(t *testing.T) {
	fake := &fakeWA{registered: map[string]string{"+31600000001": alice}}
	cs := connect(t, fake, storetest.New(t), Options{ReadOnly: true})
	var out CheckNumberOut
	if res := call(t, cs, "check_number", map[string]any{"phones": []string{"+31600000001"}}, &out); res.IsError {
		t.Fatalf("check_number in read-only mode: %v", res.Content)
	}
	if len(out.Numbers) != 1 || !out.Numbers[0].OnWhatsApp {
		t.Errorf("numbers = %+v", out.Numbers)
	}
}

func TestCheckNumberHonoursAllowRecipients(t *testing.T) {
	fake := &fakeWA{registered: map[string]string{"+31600000001": alice}}
	cs := connect(t, fake, storetest.New(t), Options{AllowRecipients: []string{"+31600000001"}})

	// An allowed number, and the account's own, may be looked up.
	var out CheckNumberOut
	if res := call(t, cs, "check_number", map[string]any{"phones": []string{"+31 6 0000 0001", "+31600000000"}}, &out); res.IsError {
		t.Fatalf("check_number on allowed numbers: %v", res.Content)
	}
	if len(out.Numbers) != 2 || !out.Numbers[0].OnWhatsApp {
		t.Errorf("numbers = %+v", out.Numbers)
	}

	// One number off the list refuses the whole call, before any lookup.
	fake.checkCalls = nil
	res := call(t, cs, "check_number", map[string]any{"phones": []string{"+31600000001", "+31600000002"}}, nil)
	if !res.IsError {
		t.Fatal("want tool error for a number outside the allowlist")
	}
	if len(fake.checkCalls) != 0 {
		t.Errorf("WhatsApp was contacted: %v", fake.checkCalls)
	}
}
