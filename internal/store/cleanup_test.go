// SPDX-License-Identifier: MPL-2.0

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/StephanMeijer/whatsapp-mcp/internal/db"
	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

const (
	cleanupGroup = "120363000000000001@g.us"
	cleanupMe    = "31600000000@s.whatsapp.net"
	cleanupAlice = "31600000001@s.whatsapp.net"
)

// seedWithReceiptsAndVotes archives two of our own polls in a group, each
// with a read receipt and a vote; "gone" is set to expire at 100.
func seedWithReceiptsAndVotes(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	gone := store.Message{ChatJID: cleanupGroup, MessageID: "gone", SenderJID: cleanupMe, Timestamp: 10, FromMe: true,
		Text: "Lunch?", MediaType: store.MediaPoll, ExpiresAt: 100}
	kept := store.Message{ChatJID: cleanupGroup, MessageID: "kept", SenderJID: cleanupMe, Timestamp: 20, FromMe: true,
		Text: "Dinner?", MediaType: store.MediaPoll}
	must(t, s.Save(ctx, nil, []store.Message{gone, kept}))
	for _, id := range []string{"gone", "kept"} {
		if _, err := s.RecordReceipt(ctx, cleanupGroup, id, cleanupAlice, store.StatusRead, time.Unix(30, 0)); err != nil {
			t.Fatal(err)
		}
		must(t, s.RecordPollVote(ctx, store.PollVote{ChatJID: cleanupGroup, PollID: id, VoterJID: cleanupAlice, Selected: "aa", VotedAt: 30}))
	}
}

// leftovers counts a message's receipts and votes.
func leftovers(t *testing.T, s *store.Store, id string) (receipts, votes int) {
	t.Helper()
	ctx := context.Background()
	rs, err := s.Receipts(ctx, cleanupGroup, id)
	must(t, err)
	vs, err := s.PollVotes(ctx, cleanupGroup, id)
	must(t, err)
	return len(rs), len(vs)
}

func TestDeletingAMessageDropsItsReceiptsAndVotes(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		seedWithReceiptsAndVotes(t, s)
		if ok, err := s.MarkDeleted(ctx, cleanupGroup, "gone"); err != nil || !ok {
			t.Fatalf("MarkDeleted = %v, %v", ok, err)
		}
		if r, v := leftovers(t, s, "gone"); r != 0 || v != 0 {
			t.Errorf("deleted message keeps %d receipts and %d votes, want none", r, v)
		}
		if r, v := leftovers(t, s, "kept"); r != 1 || v != 1 {
			t.Errorf("other message has %d receipts and %d votes, want 1 and 1", r, v)
		}

		// Late arrivals don't bring them back.
		if _, err := s.RecordReceipt(ctx, cleanupGroup, "gone", cleanupAlice, store.StatusPlayed, time.Unix(40, 0)); err != nil {
			t.Fatal(err)
		}
		must(t, s.RecordPollVote(ctx, store.PollVote{ChatJID: cleanupGroup, PollID: "gone", VoterJID: cleanupAlice, Selected: "bb", VotedAt: 40}))
		if r, v := leftovers(t, s, "gone"); r != 0 || v != 0 {
			t.Errorf("after late arrivals: %d receipts and %d votes, want none", r, v)
		}

		// A vote that arrives before its poll is archived still counts.
		must(t, s.RecordPollVote(ctx, store.PollVote{ChatJID: cleanupGroup, PollID: "not-yet", VoterJID: cleanupAlice, Selected: "cc", VotedAt: 50}))
		if _, v := leftovers(t, s, "not-yet"); v != 1 {
			t.Errorf("early vote: %d stored, want 1", v)
		}
	})
}

func TestExpiringMessagesDropsTheirReceiptsAndVotes(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		seedWithReceiptsAndVotes(t, s)
		n, err := s.ExpireMessages(ctx, 100)
		must(t, err)
		if n != 1 {
			t.Fatalf("expired %d messages, want 1", n)
		}
		if r, v := leftovers(t, s, "gone"); r != 0 || v != 0 {
			t.Errorf("expired message keeps %d receipts and %d votes, want none", r, v)
		}
		if r, v := leftovers(t, s, "kept"); r != 1 || v != 1 {
			t.Errorf("message without a timer has %d receipts and %d votes, want 1 and 1", r, v)
		}
	})
}

func TestOpeningTheStoreDropsReceiptsAndVotesOfEarlierDeletions(t *testing.T) {
	eachDB(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		s := openStore(t, d)
		seedWithReceiptsAndVotes(t, s)
		must(t, s.MarkDeletedKeepingRows(ctx, cleanupGroup, "gone"))

		s = openStore(t, d)
		if r, v := leftovers(t, s, "gone"); r != 0 || v != 0 {
			t.Errorf("deleted message keeps %d receipts and %d votes, want none", r, v)
		}
		if r, v := leftovers(t, s, "kept"); r != 1 || v != 1 {
			t.Errorf("other message has %d receipts and %d votes, want 1 and 1", r, v)
		}
	})
}
