// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestPostStatus(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	m, err := c.PostStatus(context.Background(), "Back from holiday")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 1 || fake.sent[0].to != types.StatusBroadcastJID ||
		fake.sent[0].msg.GetExtendedTextMessage().GetText() != "Back from holiday" {
		t.Fatalf("sent = %+v, want an extended text to status@broadcast", fake.sent)
	}
	if a := archived(t, c, types.StatusBroadcastJID, m.MessageID); !a.FromMe || a.Text != "Back from holiday" {
		t.Errorf("archived = %+v, want our own status", a)
	}
	fake.loggedIn = false
	if _, err := c.PostStatus(context.Background(), "x"); !errors.Is(err, errNotLoggedIn) {
		t.Errorf("logged out: err %v", err)
	}
}
