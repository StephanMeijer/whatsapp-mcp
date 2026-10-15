// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

// Phone numbers are E.164: a country code and subscriber number, at most 15
// digits. No country has a full number shorter than 7.
const (
	minPhoneDigits = 7
	maxPhoneDigits = 15
)

// NumberCheck is whether one phone number is registered on WhatsApp.
type NumberCheck struct {
	Phone        string `json:"phone" jsonschema:"the number as looked up, in international format with a leading +"`
	OnWhatsApp   bool   `json:"on_whatsapp"`
	JID          string `json:"jid,omitempty" jsonschema:"the JID to use with send_message; set only if on WhatsApp"`
	BusinessName string `json:"business_name,omitempty" jsonschema:"the verified business name, if it is a business account"`
}

// NormalizePhone turns a phone number as people write it ("+31 6 1234 5678",
// "0031 6 12345678") into "+" and its digits. The country code is required,
// as "+" or "00": a national number such as "06 12345678" is rejected rather
// than guessed at, since it would silently look up the wrong person.
func NormalizePhone(s string) (string, error) {
	rest, plus := strings.CutPrefix(strings.TrimSpace(s), "+")
	var digits strings.Builder
	for _, r := range rest {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case strings.ContainsRune(" -.()", r):
		default:
			return "", fmt.Errorf("invalid number %q: want digits with a country code, such as +31612345678", s)
		}
	}
	d := digits.String()
	if !plus {
		var ok bool
		if d, ok = strings.CutPrefix(d, "00"); !ok {
			return "", fmt.Errorf("invalid number %q: include the country code, as +31… or 0031…", s)
		}
	}
	// A country code never starts with 0.
	if len(d) < minPhoneDigits || len(d) > maxPhoneDigits || d[0] == '0' {
		return "", fmt.Errorf("invalid number %q: want a country code and %d to %d digits in all",
			s, minPhoneDigits, maxPhoneDigits)
	}
	return "+" + d, nil
}

// CheckNumbers asks WhatsApp which of phones are registered. Each phone must
// come from NormalizePhone. The result has one entry per phone, in order.
func (c *Client) CheckNumbers(ctx context.Context, phones []string) ([]NumberCheck, error) {
	if !c.cli.IsLoggedIn() {
		return nil, errors.New("not logged in to WhatsApp")
	}
	resp, err := c.cli.IsOnWhatsApp(ctx, phones)
	if err != nil {
		return nil, fmt.Errorf("look up numbers: %w", err)
	}
	return numberChecks(phones, resp, func(j types.JID) types.JID { return c.Canonical(ctx, j) }), nil
}

// numberChecks pairs each phone with its answer. WhatsApp's reply is not in
// query order, and leaves out numbers it has no account for, so answers are
// matched by the number they echo; no answer means not on WhatsApp.
func numberChecks(phones []string, resp []types.IsOnWhatsAppResponse, canon func(types.JID) types.JID) []NumberCheck {
	answers := make(map[string]types.IsOnWhatsAppResponse, len(resp))
	for _, r := range resp {
		answers[answeredDigits(r)] = r
	}
	out := make([]NumberCheck, len(phones))
	for i, phone := range phones {
		out[i] = NumberCheck{Phone: phone}
		r, ok := answers[digitsOf(phone)]
		if !ok || !r.IsIn {
			continue
		}
		out[i].OnWhatsApp = true
		// The reply may name the person by @lid; send_message wants the number.
		jid := r.PhoneNumber
		if jid.IsEmpty() {
			jid = canon(r.JID)
		}
		out[i].JID = jid.String()
		if r.VerifiedName != nil {
			out[i].BusinessName = r.VerifiedName.Details.GetVerifiedName()
		}
	}
	return out
}

// answeredDigits is the number an answer is about: the one it echoes, or
// failing that the one in its JID.
func answeredDigits(r types.IsOnWhatsAppResponse) string {
	if r.Query != "" {
		return digitsOf(r.Query)
	}
	if !r.PhoneNumber.IsEmpty() {
		return r.PhoneNumber.User
	}
	return r.JID.User
}

func digitsOf(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}
