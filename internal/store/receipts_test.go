// SPDX-License-Identifier: MPL-2.0

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestReceipts(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const me, alice, bob, group = "0@s.whatsapp.net", "1@s.whatsapp.net", "2@s.whatsapp.net", "g@g.us"
		must(t, s.Save(ctx, nil, []store.Message{
			{ChatJID: alice, MessageID: "own", SenderJID: me, Timestamp: 10, FromMe: true, Text: "hi"},
			{ChatJID: alice, MessageID: "note", SenderJID: me, Timestamp: 11, FromMe: true, MediaType: "audio"},
			{ChatJID: alice, MessageID: "theirs", SenderJID: alice, Timestamp: 12, Text: "hello"},
			{ChatJID: group, MessageID: "g1", SenderJID: me, Timestamp: 13, FromMe: true, Text: "all"},
		}))
		status := func(chat, id string) string {
			t.Helper()
			m, err := s.MessageByID(ctx, chat, id)
			must(t, err)
			return m.Status
		}
		record := func(chat, id, who, st string, sec int64) bool {
			t.Helper()
			moved, err := s.RecordReceipt(ctx, chat, id, who, st, time.Unix(sec, 0))
			must(t, err)
			return moved
		}

		if got := status(alice, "own"); got != store.StatusSent {
			t.Fatalf("new message status = %q, want sent", got)
		}
		// Forward: delivered, then read.
		if !record(alice, "own", alice, store.StatusDelivered, 100) || status(alice, "own") != store.StatusDelivered {
			t.Errorf("after delivered: %q", status(alice, "own"))
		}
		if !record(alice, "own", alice, store.StatusRead, 200) || status(alice, "own") != store.StatusRead {
			t.Errorf("after read: %q", status(alice, "own"))
		}
		// A late or repeated delivered neither moves back nor overwrites the time.
		if record(alice, "own", alice, store.StatusDelivered, 300) || status(alice, "own") != store.StatusRead {
			t.Errorf("late delivered moved status to %q", status(alice, "own"))
		}
		rs, err := s.Receipts(ctx, alice, "own")
		must(t, err)
		if len(rs) != 1 || rs[0].ParticipantJID != alice || rs[0].DeliveredAt != 100 || rs[0].ReadAt != 200 || rs[0].PlayedAt != 0 {
			t.Errorf("receipts = %+v", rs)
		}

		// Out of order: read arrives before delivered.
		record(alice, "note", alice, store.StatusRead, 400)
		record(alice, "note", alice, store.StatusDelivered, 350)
		if got := status(alice, "note"); got != store.StatusRead {
			t.Errorf("read then delivered = %q, want read", got)
		}
		if !record(alice, "note", alice, store.StatusPlayed, 500) || status(alice, "note") != store.StatusPlayed {
			t.Errorf("after played: %q", status(alice, "note"))
		}

		// Not ours, or not archived: ignored, no rows.
		if record(alice, "theirs", alice, store.StatusRead, 600) || status(alice, "theirs") != store.StatusSent {
			t.Error("a receipt for an incoming message was applied")
		}
		if record(alice, "nope", alice, store.StatusRead, 600) {
			t.Error("a receipt for an unknown message was applied")
		}
		for _, id := range []string{"theirs", "nope"} {
			if rs, _ := s.Receipts(ctx, alice, id); len(rs) != 0 {
				t.Errorf("receipts for %s = %+v, want none", id, rs)
			}
		}

		// Group: one row per member, status is the furthest anyone reached.
		record(group, "g1", bob, store.StatusDelivered, 700)
		record(group, "g1", alice, store.StatusRead, 710)
		record(group, "g1", bob, store.StatusRead, 720)
		rs, err = s.Receipts(ctx, group, "g1")
		must(t, err)
		if len(rs) != 2 || rs[0].ParticipantJID != alice || rs[0].ReadAt != 710 ||
			rs[1].ParticipantJID != bob || rs[1].DeliveredAt != 700 || rs[1].ReadAt != 720 {
			t.Errorf("group receipts = %+v", rs)
		}
		if got := status(group, "g1"); got != store.StatusRead {
			t.Errorf("group status = %q, want read", got)
		}

		if _, err := s.RecordReceipt(ctx, alice, "own", alice, "bogus", time.Unix(1, 0)); err == nil {
			t.Error("bogus status accepted")
		}
	})
}
