// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// vcardPhones returns the phone numbers on a vCard's TEL lines, as written.
func vcardPhones(vcard string) []string {
	var phones []string
	for line := range strings.Lines(vcard) {
		prop, value, ok := strings.Cut(strings.TrimRight(line, "\r\n"), ":")
		if !ok {
			continue
		}
		// A property may be grouped, as in "item1.TEL;waid=...".
		if _, after, grouped := strings.Cut(prop, "."); grouped {
			prop = after
		}
		name, _, _ := strings.Cut(prop, ";")
		if strings.EqualFold(name, "TEL") && strings.TrimSpace(value) != "" {
			phones = append(phones, strings.TrimSpace(value))
		}
	}
	return phones
}

// contactCardText is one card's archived line: the name, then its numbers.
func contactCardText(c *waE2E.ContactMessage) string {
	phones := strings.Join(vcardPhones(c.GetVcard()), ", ")
	if phones == "" {
		return c.GetDisplayName()
	}
	return c.GetDisplayName() + ": " + phones
}

// contactContent returns the archived text of a contact card or a list of
// them, one line per card, or false if m is neither.
func contactContent(m *waE2E.Message) (string, bool) {
	if c := m.GetContactMessage(); c != nil {
		return contactCardText(c), true
	}
	if a := m.GetContactsArrayMessage(); a != nil {
		lines := make([]string, len(a.GetContacts()))
		for i, c := range a.GetContacts() {
			lines[i] = contactCardText(c)
		}
		return strings.Join(lines, "\n"), true
	}
	return "", false
}

// vcardEscaper escapes a vCard 3.0 text value.
var vcardEscaper = strings.NewReplacer(`\`, `\\`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`, ",", `\,`, ";", `\;`)

// contactVCard is the minimal vCard WhatsApp shows as a tappable contact;
// waid is what links it to the WhatsApp account. phone is from NormalizePhone.
func contactVCard(name, phone string) string {
	return "BEGIN:VCARD\nVERSION:3.0\nFN:" + vcardEscaper.Replace(name) +
		"\nTEL;type=CELL;waid=" + strings.TrimPrefix(phone, "+") + ":" + phone + "\nEND:VCARD"
}

// SendContact sends a contact card for name and phone, and archives it.
func (c *Client) SendContact(ctx context.Context, recipient, name, phone string) (store.Message, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.Message{}, errors.New("the contact's name is required")
	}
	phone, err := NormalizePhone(phone)
	if err != nil {
		return store.Message{}, err
	}
	to, err := c.recipient(recipient)
	if err != nil {
		return store.Message{}, err
	}
	msg := &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
		DisplayName: proto.String(name),
		Vcard:       proto.String(contactVCard(name, phone)),
	}}
	return c.sendAndArchive(ctx, to, msg, "contact")
}
