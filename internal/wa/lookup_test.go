// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"slices"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waVnameCert"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		in   string
		want string // empty: must be rejected
	}{
		{"+31612345678", "+31612345678"},
		{"+31 6 1234 5678", "+31612345678"},
		{"  +31-6-12345678 ", "+31612345678"},
		{"0031 6 12345678", "+31612345678"},
		{"+1 (415) 555-0100", "+14155550100"},
		{"+44.7911.123456", "+447911123456"},
		{"+6831234", "+6831234"},                 // 7 digits: the shortest allowed
		{"+123456789012345", "+123456789012345"}, // 15 digits: the longest allowed
		{"0612345678", ""},                       // national number, no country code
		{"31612345678", ""},                      // digits only: country code not marked
		{"612345678", ""},                        // could be Spanish or another country's
		{"+", ""},
		{"00", ""},
		{"+0612345678", ""}, // a country code never starts with 0
		{"000612345678", ""},
		{"+316123", ""},           // 6 digits
		{"+1234567890123456", ""}, // 16 digits
		{"+31 6 abcd", ""},
		{"+31+612345678", ""},
		{"31612345678@s.whatsapp.net", ""}, // a JID is not a number
		{"+31612345678@s.whatsapp.net", ""},
		{"", ""},
		{"   ", ""},
	}
	for _, tt := range tests {
		got, err := NormalizePhone(tt.in)
		if tt.want == "" {
			if err == nil {
				t.Errorf("NormalizePhone(%q) = %q, want error", tt.in, got)
			} else if !strings.Contains(err.Error(), tt.in) && strings.TrimSpace(tt.in) != "" {
				t.Errorf("NormalizePhone(%q) error %q does not name the input", tt.in, err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("NormalizePhone(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestNumberChecks(t *testing.T) {
	lid := types.NewJID("111", types.HiddenUserServer)
	otherLID := types.NewJID("222", types.HiddenUserServer)
	pn := func(n string) types.JID { return types.NewJID(n, types.DefaultUserServer) }
	canon := func(j types.JID) types.JID {
		if j == lid {
			return pn("31600000001")
		}
		return j
	}
	business := &types.VerifiedName{Details: &waVnameCert.VerifiedNameCertificate_Details{VerifiedName: proto.String("Acme BV")}}

	phones := []string{"+31600000001", "+31600000002", "+31600000003", "+31600000004", "+31600000005", "+31600000006"}
	// WhatsApp answers in its own order, leaves out the number it has no
	// account for, and may name people by @lid.
	resp := append([]types.IsOnWhatsAppResponse(nil),
		types.IsOnWhatsAppResponse{Query: "+31600000005", JID: pn("31600000005"), IsIn: true, VerifiedName: business},
		types.IsOnWhatsAppResponse{Query: "+31600000002", JID: otherLID, PhoneNumber: pn("31600000002"), IsIn: true},
		types.IsOnWhatsAppResponse{Query: "+31600000001", JID: lid, IsIn: true},
		types.IsOnWhatsAppResponse{Query: "+31600000003", JID: pn("31600000003"), IsIn: false},
		// 31600000006 has no Query echo: matched by its phone number instead.
		types.IsOnWhatsAppResponse{JID: pn("31600000006"), IsIn: true},
		// 31600000004 is absent.
	)

	want := []NumberCheck{
		{Phone: "+31600000001", OnWhatsApp: true, JID: "31600000001@s.whatsapp.net"}, // @lid resolved by canon
		{Phone: "+31600000002", OnWhatsApp: true, JID: "31600000002@s.whatsapp.net"}, // @lid with its number in the reply
		{Phone: "+31600000003"},
		{Phone: "+31600000004"},
		{Phone: "+31600000005", OnWhatsApp: true, JID: "31600000005@s.whatsapp.net", BusinessName: "Acme BV"},
		{Phone: "+31600000006", OnWhatsApp: true, JID: "31600000006@s.whatsapp.net"},
	}
	got := numberChecks(phones, resp, canon)
	if !slices.Equal(got, want) {
		t.Errorf("numberChecks =\n %+v\nwant\n %+v", got, want)
	}
}

func TestNumberChecksWithoutAnyAnswer(t *testing.T) {
	got := numberChecks([]string{"+31600000001"}, nil, func(j types.JID) types.JID { return j })
	if want := []NumberCheck{{Phone: "+31600000001"}}; !slices.Equal(got, want) {
		t.Errorf("numberChecks = %+v, want %+v", got, want)
	}
}
