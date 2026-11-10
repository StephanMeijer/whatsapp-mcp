// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// lookUp runs Presence for Alice's number and delivers evts once it waits.
func lookUp(t *testing.T, c *Client, evts ...any) Presence {
	t.Helper()
	got := make(chan Presence, 1)
	go func() {
		p, err := c.Presence(context.Background(), alicePN)
		if err != nil {
			t.Error(err)
		}
		got <- p
	}()
	synctest.Wait()
	for _, e := range evts {
		c.handleEvent(e)
	}
	return <-got
}

func TestPresence(t *testing.T) {
	seen := time.Unix(1_700_000_000, 0)
	typing := &events.ChatPresence{
		MessageSource: types.MessageSource{Chat: aliceLID, Sender: aliceLID},
		State:         types.ChatPresenceComposing,
	}
	recording := &events.ChatPresence{
		MessageSource: types.MessageSource{Chat: alicePN, Sender: alicePN},
		State:         types.ChatPresenceComposing, Media: types.ChatPresenceMediaAudio,
	}
	inGroup := &events.ChatPresence{
		MessageSource: types.MessageSource{Chat: testGroup, Sender: alicePN, IsGroup: true},
		State:         types.ChatPresenceComposing,
	}
	for name, tc := range map[string]struct {
		evts []any
		want Presence
	}{
		"online":           {[]any{&events.Presence{From: alicePN}}, Presence{State: "online"}},
		"offline":          {[]any{&events.Presence{From: alicePN, Unavailable: true, LastSeen: seen}}, Presence{State: "offline", LastSeen: "2023-11-14T22:13:20Z"}},
		"by lid, typing":   {[]any{typing, &events.Presence{From: aliceLID}}, Presence{State: "online", ChatState: "typing"}},
		"recording":        {[]any{recording, &events.Presence{From: alicePN}}, Presence{State: "online", ChatState: "recording"}},
		"hidden":           {nil, Presence{State: "not_visible"}},
		"someone else":     {[]any{&events.Presence{From: bobPN}}, Presence{State: "not_visible"}},
		"typing in group":  {[]any{inGroup, &events.Presence{From: alicePN}}, Presence{State: "online"}},
		"typing, hides it": {[]any{typing}, Presence{State: "not_visible", ChatState: "typing"}},
	} {
		c, fake, _, _ := newTestClient(t) // outside the bubble: the archive does real I/O
		synctest.Test(t, func(t *testing.T) {
			tc.want.JID = alicePN.String()
			if got := lookUp(t, c, tc.evts...); got != tc.want {
				t.Errorf("%s: got %+v, want %+v", name, got, tc.want)
			}
			if want := []string{"available", "subscribe " + alicePN.String()}; !slices.Equal(fake.presenceCalls(), want) {
				t.Errorf("%s: WhatsApp got %v, want %v", name, fake.presenceCalls(), want)
			}
			// The lease ends: the account goes offline again.
			time.Sleep(onlineLease)
			synctest.Wait()
			if got := fake.presenceCalls(); got[len(got)-1] != "unavailable" {
				t.Errorf("%s: after the lease WhatsApp got %q, want unavailable last", name, got)
			}
		})
	}
}

func TestSetTypingExtendsTheLease(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		if err := c.SetTyping(ctx, alicePN, types.ChatPresenceComposing, types.ChatPresenceMediaAudio); err != nil {
			t.Fatal(err)
		}
		time.Sleep(onlineLease / 2)
		if err := c.SetTyping(ctx, alicePN, types.ChatPresencePaused, types.ChatPresenceMediaText); err != nil {
			t.Fatal(err)
		}
		// The first call's lease would end here; the second extended it.
		time.Sleep(onlineLease/2 + time.Second)
		synctest.Wait()
		want := []string{
			"available", alicePN.String() + " composing audio",
			"available", alicePN.String() + " paused ",
		}
		if !slices.Equal(fake.presenceCalls(), want) {
			t.Errorf("WhatsApp got %q, want %q", fake.presenceCalls(), want)
		}
		time.Sleep(onlineLease)
		synctest.Wait()
		if want = append(want, "unavailable"); !slices.Equal(fake.presenceCalls(), want) {
			t.Errorf("after the lease WhatsApp got %q, want %q", fake.presenceCalls(), want)
		}
	})
}

func TestPresenceNeedsLogin(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	fake.loggedIn = false
	if _, err := c.Presence(context.Background(), alicePN); err != errNotLoggedIn {
		t.Errorf("Presence: %v, want errNotLoggedIn", err)
	}
	if err := c.SetTyping(context.Background(), alicePN, types.ChatPresencePaused, ""); err != errNotLoggedIn {
		t.Errorf("SetTyping: %v, want errNotLoggedIn", err)
	}
	if got := fake.presenceCalls(); len(got) != 0 {
		t.Errorf("WhatsApp got %v", got)
	}
}
