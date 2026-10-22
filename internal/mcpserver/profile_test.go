// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

var aliceInfo = wa.ContactInfo{JID: alice, Name: "Alice", About: "Out of office", HasPicture: true}

func TestContactInfo(t *testing.T) {
	for _, opts := range []Options{{}, {ReadOnly: true}} {
		fake := &fakeWA{contacts: map[string]wa.ContactInfo{alice: aliceInfo}}
		cs := connect(t, fake, storetest.New(t), opts)
		// By JID and by phone number: both reach WhatsApp as the JID.
		for _, in := range []string{alice, "+31 6 0000 0001"} {
			var out ContactInfoOut
			if res := call(t, cs, "contact_info", map[string]any{"jid": in}, &out); res.IsError {
				t.Fatalf("contact_info(%q, read-only=%v): %v", in, opts.ReadOnly, res.Content)
			}
			if out.Contact != aliceInfo {
				t.Errorf("contact_info(%q) = %+v, want %+v", in, out.Contact, aliceInfo)
			}
		}
		if want := []string{alice, alice}; !slices.Equal(fake.contactCalls, want) {
			t.Errorf("WhatsApp was asked about %v, want %v", fake.contactCalls, want)
		}
	}
}

func TestContactInfoErrors(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{})
	if res := call(t, cs, "contact_info", map[string]any{}, nil); !res.IsError {
		t.Error("contact_info without jid: want tool error")
	}
	if res := call(t, cs, "contact_info", map[string]any{"jid": "not a jid"}, nil); !res.IsError {
		t.Error("contact_info with garbage: want tool error")
	}
	if len(fake.contactCalls) != 0 {
		t.Errorf("WhatsApp was asked: %v", fake.contactCalls)
	}
	res := call(t, cs, "contact_info", map[string]any{"jid": alice}, nil)
	if got := res.Content[0].(*mcp.TextContent).Text; !res.IsError || !strings.Contains(got, "no info") {
		t.Errorf("unknown contact: error = %v %q, want the lookup's reason", res.IsError, got)
	}
}

func TestContactInfoHonoursAllowRecipients(t *testing.T) {
	self := "31600000000@s.whatsapp.net"
	fake := &fakeWA{contacts: map[string]wa.ContactInfo{alice: aliceInfo, self: {JID: self}}}
	cs := connect(t, fake, storetest.New(t), Options{AllowRecipients: []string{alice}})
	for _, in := range []string{alice, self} { // allowed, and the account's own
		if res := call(t, cs, "contact_info", map[string]any{"jid": in}, nil); res.IsError {
			t.Errorf("contact_info(%q): %v", in, res.Content)
		}
	}
	fake.contactCalls = nil
	if res := call(t, cs, "contact_info", map[string]any{"jid": "31600000002@s.whatsapp.net"}, nil); !res.IsError {
		t.Error("want tool error for a JID outside the allowlist")
	}
	if res := call(t, cs, "get_profile_picture", map[string]any{"jid": "31600000002@s.whatsapp.net"}, nil); !res.IsError {
		t.Error("want tool error for a picture outside the allowlist")
	}
	if len(fake.contactCalls)+len(fake.pictureCalls) != 0 {
		t.Errorf("WhatsApp was contacted: %v %v", fake.contactCalls, fake.pictureCalls)
	}
}

func TestGetProfilePicture(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 'j', 'p', 'g'}
	fake := &fakeWA{picture: wa.Media{Kind: "image", MIMEType: "image/jpeg", Data: jpeg}}
	cs := connect(t, fake, storetest.New(t), Options{ReadOnly: true})

	res := call(t, cs, "get_profile_picture", map[string]any{"jid": alice}, nil)
	if res.IsError || len(res.Content) != 2 {
		t.Fatalf("result = %+v", res)
	}
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "image/jpeg") {
		t.Errorf("description = %q", text)
	}
	if img, ok := res.Content[1].(*mcp.ImageContent); !ok || img.MIMEType != "image/jpeg" || !bytes.Equal(img.Data, jpeg) {
		t.Errorf("image = %+v", res.Content[1])
	}

	// A group, as a preview.
	if res := call(t, cs, "get_profile_picture", map[string]any{"jid": "123@g.us", "preview": true}, nil); res.IsError {
		t.Errorf("group picture: %v", res.Content)
	}
	if want := []string{alice + " preview=false", "123@g.us preview=true"}; !slices.Equal(fake.pictureCalls, want) {
		t.Errorf("WhatsApp was asked for %v, want %v", fake.pictureCalls, want)
	}
}

func TestGetProfilePictureHiddenOrMissing(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want string
	}{{wa.ErrPictureHidden, "hidden"}, {wa.ErrNoPicture, "no profile picture"}} {
		cs := connect(t, &fakeWA{pictureErr: tt.err}, storetest.New(t), Options{})
		res := call(t, cs, "get_profile_picture", map[string]any{"jid": alice}, nil)
		if got := res.Content[0].(*mcp.TextContent).Text; !res.IsError || !strings.Contains(got, tt.want) {
			t.Errorf("error = %v %q, want %q", res.IsError, got, tt.want)
		}
	}
}
