// SPDX-License-Identifier: MPL-2.0

package store_test

import (
	"context"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func votesBy(t *testing.T, s *store.Store, chat, poll string) map[string]store.PollVote {
	t.Helper()
	votes, err := s.PollVotes(context.Background(), chat, poll)
	must(t, err)
	out := map[string]store.PollVote{}
	for _, v := range votes {
		out[v.VoterJID] = v
	}
	return out
}

func TestPollVotes(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const chat, poll = "120363000000000001@g.us", "P1"
		const alice, bob = "1@s.whatsapp.net", "2@s.whatsapp.net"
		vote := func(voter, selected string, at int64) {
			t.Helper()
			must(t, s.RecordPollVote(ctx, store.PollVote{ChatJID: chat, PollID: poll, VoterJID: voter, Selected: selected, VotedAt: at}))
		}
		vote(alice, "aa", 10)
		vote(alice, "bb", 20) // changed her mind
		vote(alice, "aa", 15) // an older vote arriving late does not undo that
		vote(bob, "aa,bb", 10)
		vote(bob, "", 30) // withdrew
		must(t, s.RecordPollVote(ctx, store.PollVote{ChatJID: chat, PollID: "other", VoterJID: alice, Selected: "cc", VotedAt: 5}))

		got := votesBy(t, s, chat, poll)
		if len(got) != 2 || got[alice].Selected != "bb" || got[alice].VotedAt != 20 || got[bob].Selected != "" {
			t.Errorf("votes = %+v", got)
		}
	})
}

func TestMergeJIDsMovesPollVotes(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const lid, pn = "111@lid", "31600000001@s.whatsapp.net"
		const group = "120363000000000001@g.us"
		for _, v := range []store.PollVote{
			// The same person voted under both JIDs; the newer answer wins.
			{ChatJID: group, PollID: "P1", VoterJID: lid, Selected: "new", VotedAt: 20},
			{ChatJID: group, PollID: "P1", VoterJID: pn, Selected: "old", VotedAt: 10},
			// A poll in the direct chat archived under the LID.
			{ChatJID: lid, PollID: "P2", VoterJID: lid, Selected: "x", VotedAt: 5},
		} {
			must(t, s.RecordPollVote(ctx, v))
		}
		if _, err := s.MergeJIDs(ctx, map[string]string{lid: pn}); err != nil {
			t.Fatal(err)
		}
		if got := votesBy(t, s, group, "P1"); len(got) != 1 || got[pn].Selected != "new" {
			t.Errorf("group poll votes = %+v, want only the newer vote under the number", got)
		}
		if got := votesBy(t, s, pn, "P2"); len(got) != 1 || got[pn].Selected != "x" {
			t.Errorf("direct chat poll votes = %+v, want it moved to the number", got)
		}
		if got := votesBy(t, s, lid, "P2"); len(got) != 0 {
			t.Errorf("votes left under the LID chat: %+v", got)
		}
	})
}
