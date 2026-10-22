// SPDX-License-Identifier: MPL-2.0

package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/StephanMeijer/whatsapp-mcp/internal/db"
	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

// eachDialect runs fn against SQLite, and against Postgres when
// TEST_POSTGRES_URL points at a database the test may freely write to.
func eachDialect(t *testing.T, fn func(t *testing.T, s *store.Store)) {
	t.Run("sqlite", func(t *testing.T) {
		fn(t, storetest.New(t))
	})
	t.Run("postgres", func(t *testing.T) {
		url := os.Getenv("TEST_POSTGRES_URL")
		if url == "" {
			t.Skip("TEST_POSTGRES_URL not set")
		}
		d, err := db.Open(url)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = d.Close() })
		// Start from empty tables so subtests don't see each other's rows.
		if err := d.Gorm.Exec("DROP TABLE IF EXISTS messages, chats, calls, receipts").Error; err != nil {
			t.Fatal(err)
		}
		s, err := store.New(context.Background(), d)
		if err != nil {
			t.Fatal(err)
		}
		fn(t, s)
	})
}

func msg(chat, id string, ts int64, text string) store.Message {
	return store.Message{ChatJID: chat, MessageID: id, SenderJID: "1@s.whatsapp.net", Timestamp: ts, Text: text}
}

func ids(msgs []store.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.MessageID
	}
	return out
}

func TestSaveIsIdempotentAndMergesChats(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const alice = "31600000001@s.whatsapp.net"

		must(t, s.Save(ctx, []store.Chat{{JID: alice, Name: "Alice"}}, []store.Message{msg(alice, "A1", 100, "hello")}))
		// Replay, plus a newer message and an empty name that must not clobber "Alice".
		must(t, s.Save(ctx, []store.Chat{{JID: alice}}, []store.Message{msg(alice, "A1", 100, "hello"), msg(alice, "A2", 200, "again")}))
		// An older batch (late history sync) must not move LastMessageAt back.
		must(t, s.Save(ctx, nil, []store.Message{msg(alice, "A0", 50, "first")}))

		chats, err := s.ListChats(ctx, "", store.IncludeArchived, 0)
		must(t, err)
		if len(chats) != 1 {
			t.Fatalf("got %d chats, want 1: %+v", len(chats), chats)
		}
		if got := chats[0]; got.Name != "Alice" || got.LastMessageAt != 200 {
			t.Errorf("chat = %+v, want name Alice, last 200", got)
		}

		msgs, err := s.ListMessages(ctx, alice, time.Time{}, 0)
		must(t, err)
		if got, want := ids(msgs), []string{"A2", "A1", "A0"}; !slices.Equal(got, want) {
			t.Errorf("messages = %v, want %v", got, want)
		}
	})
}

func TestRenameChats(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		must(t, s.Save(ctx, []store.Chat{
			{JID: "1@s.whatsapp.net", LastMessageAt: 10},
			{JID: "2@s.whatsapp.net", Name: "Bob", LastMessageAt: 20},
		}, nil))

		changed, err := s.RenameChats(ctx, map[string]string{
			"1@s.whatsapp.net": "Alice",
			"2@s.whatsapp.net": "Bob", // unchanged
			"3@s.whatsapp.net": "Carol",
			"4@s.whatsapp.net": "", // ignored
		})
		must(t, err)
		if changed != 1 {
			t.Errorf("changed = %d, want 1", changed)
		}

		chats, err := s.ListChats(ctx, "", store.IncludeArchived, 0)
		must(t, err)
		got := []string{}
		for _, c := range chats {
			got = append(got, c.JID+"="+c.Name)
		}
		if want := []string{"2@s.whatsapp.net=Bob", "1@s.whatsapp.net=Alice"}; !slices.Equal(got, want) {
			t.Errorf("chats = %v, want %v (unknown JIDs must not create chats)", got, want)
		}
	})
}

func TestMergeJIDs(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const (
			lidA = "111@lid" // has a PN chat already: must be merged
			pnA  = "31600000001@s.whatsapp.net"
			lidB = "222@lid" // no PN chat yet: only renamed
			pnB  = "31600000002@s.whatsapp.net"
			lidC = "333@lid" // unmapped: untouched
			lidS = "444@lid" // only appears as a group sender
			pnS  = "31600000004@s.whatsapp.net"
			grp  = "120363000000000000@g.us"
		)
		lidMsg := func(chat, id string, ts int64, text, sender string) store.Message {
			m := msg(chat, id, ts, text)
			m.SenderJID = sender
			return m
		}
		must(t, s.Save(ctx,
			[]store.Chat{{JID: pnA, Name: "Alice"}, {JID: lidA, Name: "alice push name"}, {JID: lidB, Name: "Bob"}},
			[]store.Message{
				lidMsg(pnA, "dup", 100, "same message seen twice", pnA),
				lidMsg(pnA, "a1", 110, "via phone number", pnA),
				lidMsg(lidA, "dup", 100, "same message seen twice", lidA),
				lidMsg(lidA, "a2", 300, "via lid avocado", lidA),
				lidMsg(lidB, "b1", 200, "bob here", lidB),
				lidMsg(lidC, "c1", 50, "unknown", lidC),
				lidMsg(grp, "g1", 400, "in a group", lidS),
			}))

		lids, err := s.LIDJIDs(ctx)
		must(t, err)
		slices.Sort(lids)
		if want := []string{lidA, lidB, lidC, lidS}; !slices.Equal(lids, want) {
			t.Fatalf("LIDJIDs = %v, want %v", lids, want)
		}

		renames := map[string]string{lidA: pnA, lidB: pnB, lidS: pnS}
		n, err := s.MergeJIDs(ctx, renames)
		must(t, err)
		if n != 3 {
			t.Errorf("merged = %d, want 3", n)
		}

		chats, err := s.ListChats(ctx, "", store.IncludeArchived, 0)
		must(t, err)
		got := map[string]store.Chat{}
		for _, c := range chats {
			got[c.JID] = c
		}
		if len(got) != 4 {
			t.Errorf("chats = %v, want pnA, pnB, lidC and the group", chats)
		}
		if c := got[pnA]; c.Name != "Alice" || c.LastMessageAt != 300 {
			t.Errorf("merged chat = %+v, want name Alice (target wins) and last 300", c)
		}
		if c := got[pnB]; c.Name != "Bob" || c.LastMessageAt != 200 {
			t.Errorf("renamed chat = %+v, want name Bob carried over and last 200", c)
		}
		if _, ok := got[lidA]; ok {
			t.Error("old @lid chat still present")
		}

		msgs, err := s.ListMessages(ctx, pnA, time.Time{}, 0)
		must(t, err)
		if got, want := ids(msgs), []string{"a2", "a1", "dup"}; !slices.Equal(got, want) {
			t.Errorf("merged messages = %v, want %v (duplicate kept once)", got, want)
		}
		for _, m := range msgs {
			if m.SenderJID != pnA {
				t.Errorf("message %s sender = %s, want %s", m.MessageID, m.SenderJID, pnA)
			}
		}
		groupMsgs, err := s.ListMessages(ctx, grp, time.Time{}, 0)
		must(t, err)
		if len(groupMsgs) != 1 || groupMsgs[0].SenderJID != pnS {
			t.Errorf("group sender not rewritten: %+v", groupMsgs)
		}

		found, err := s.SearchMessages(ctx, "avocado", "", 0)
		must(t, err)
		if len(found) != 1 || found[0].ChatJID != pnA {
			t.Errorf("search after merge = %+v, want a2 in %s", found, pnA)
		}

		n, err = s.MergeJIDs(ctx, renames)
		must(t, err)
		if n != 0 {
			t.Errorf("second merge = %d, want 0 (idempotent)", n)
		}
	})
}

func TestMergeJIDsMovesReceipts(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const (
			me     = "0@s.whatsapp.net"
			lidA   = "111@lid"
			pnA    = "31600000001@s.whatsapp.net"
			lidP   = "444@lid"
			pnP    = "31600000004@s.whatsapp.net"
			group  = "120363000000000000@g.us"
			sentAt = 10
		)
		own := func(chat, id string) store.Message {
			return store.Message{ChatJID: chat, MessageID: id, SenderJID: me, Timestamp: sentAt, FromMe: true, Text: id}
		}
		must(t, s.Save(ctx, nil, []store.Message{
			own(pnA, "dup"), own(lidA, "dup"), own(lidA, "moved"), own(group, "g1"),
		}))
		rec := func(chat, id, who, st string, sec int64) {
			t.Helper()
			_, err := s.RecordReceipt(ctx, chat, id, who, st, time.Unix(sec, 0))
			must(t, err)
		}
		rec(lidA, "dup", lidA, store.StatusRead, 50)
		rec(lidA, "moved", lidA, store.StatusDelivered, 20)
		rec(lidA, "moved", lidA, store.StatusRead, 30)
		// A member seen under both names: the times are combined.
		rec(group, "g1", lidP, store.StatusDelivered, 40)
		rec(group, "g1", lidP, store.StatusRead, 90)
		rec(group, "g1", pnP, store.StatusDelivered, 60)

		_, err := s.MergeJIDs(ctx, map[string]string{lidA: pnA, lidP: pnP})
		must(t, err)

		rs, err := s.Receipts(ctx, pnA, "moved")
		must(t, err)
		if len(rs) != 1 || rs[0].ChatJID != pnA || rs[0].ParticipantJID != pnA || rs[0].DeliveredAt != 20 || rs[0].ReadAt != 30 {
			t.Errorf("moved receipts = %+v, want one under %s", rs, pnA)
		}
		if rs, _ := s.Receipts(ctx, lidA, "moved"); len(rs) != 0 {
			t.Errorf("receipts left under the old chat: %+v", rs)
		}
		if rs, _ := s.Receipts(ctx, pnA, "dup"); len(rs) != 0 {
			t.Errorf("receipt of a dropped duplicate survived: %+v", rs)
		}
		rs, err = s.Receipts(ctx, group, "g1")
		must(t, err)
		if len(rs) != 1 || rs[0].ParticipantJID != pnP || rs[0].DeliveredAt != 60 || rs[0].ReadAt != 90 {
			t.Errorf("group receipts = %+v, want one for %s with delivered 60, read 90", rs, pnP)
		}
	})
}

func TestListChats(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		must(t, s.Save(ctx, []store.Chat{
			{JID: "1@s.whatsapp.net", Name: "Alice", LastMessageAt: 10},
			{JID: "2@s.whatsapp.net", Name: "Bob", LastMessageAt: 30},
			{JID: "3@g.us", Name: "100% Family", LastMessageAt: 20},
		}, nil))

		tests := []struct {
			query string
			limit int
			want  []string
		}{
			{query: "", want: []string{"Bob", "100% Family", "Alice"}},
			{query: "", limit: 2, want: []string{"Bob", "100% Family"}},
			{query: "ALI", want: []string{"Alice"}},
			{query: "g.us", want: []string{"100% Family"}},
			{query: "%", want: []string{"100% Family"}}, // LIKE wildcards are literal
			{query: "nobody", want: []string{}},
		}
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%q/%d", tt.query, tt.limit), func(t *testing.T) {
				chats, err := s.ListChats(ctx, tt.query, store.IncludeArchived, tt.limit)
				must(t, err)
				got := []string{}
				for _, c := range chats {
					got = append(got, c.Name)
				}
				if !slices.Equal(got, tt.want) {
					t.Errorf("ListChats(%q, %d) = %v, want %v", tt.query, tt.limit, got, tt.want)
				}
			})
		}
	})
}

func TestListMessagesPaging(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const chat = "1@s.whatsapp.net"
		must(t, s.Save(ctx, nil, []store.Message{
			msg(chat, "m1", 100, "a"), msg(chat, "m2", 200, "b"), msg(chat, "m3", 300, "c"),
			msg("other@s.whatsapp.net", "x", 250, "not this chat"),
		}))

		page, err := s.ListMessages(ctx, chat, time.Time{}, 2)
		must(t, err)
		if got, want := ids(page), []string{"m3", "m2"}; !slices.Equal(got, want) {
			t.Fatalf("first page = %v, want %v", got, want)
		}
		page, err = s.ListMessages(ctx, chat, time.Unix(page[len(page)-1].Timestamp, 0), 2)
		must(t, err)
		if got, want := ids(page), []string{"m1"}; !slices.Equal(got, want) {
			t.Errorf("second page = %v, want %v", got, want)
		}
	})
}

func TestSearchMessages(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		must(t, s.Save(ctx, nil, []store.Message{
			msg("1@s.whatsapp.net", "m1", 100, "Dinner at eight tonight?"),
			msg("1@s.whatsapp.net", "m2", 200, "Running late for dinner"),
			msg("2@s.whatsapp.net", "m3", 300, "dinner plans"),
			msg("2@s.whatsapp.net", "m4", 400, "unrelated"),
		}))

		tests := []struct {
			query, chat string
			want        []string
		}{
			{query: "dinner", want: []string{"m1", "m2", "m3"}},
			{query: "DINNER tonight", want: []string{"m1"}},
			{query: "dinner", chat: "2@s.whatsapp.net", want: []string{"m3"}},
			{query: `"unbalanced quote AND (`, want: nil}, // must not be a syntax error
		}
		for _, tt := range tests {
			t.Run(tt.query, func(t *testing.T) {
				msgs, err := s.SearchMessages(ctx, tt.query, tt.chat, 0)
				must(t, err)
				got := ids(msgs)
				slices.Sort(got)
				if !slices.Equal(got, tt.want) && !(len(got) == 0 && len(tt.want) == 0) {
					t.Errorf("SearchMessages(%q, %q) = %v, want %v", tt.query, tt.chat, got, tt.want)
				}
			})
		}

		if _, err := s.SearchMessages(ctx, "  ", "", 0); err == nil {
			t.Error("empty query: want error")
		}
	})
}

func TestRecentMessages(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const a, b = "1@s.whatsapp.net", "2@s.whatsapp.net"
		mine := msg(a, "mine", 150, "my own")
		mine.FromMe = true
		must(t, s.Save(ctx, []store.Chat{{JID: a, Name: "Alice"}}, []store.Message{
			msg(a, "old", 99, "before the window"),
			msg(a, "a1", 100, "exactly at since"),
			mine,
			msg(b, "b1", 160, "bob"),
			msg(a, "a2", 199, "last second"),
			msg(a, "late", 200, "exactly at until"),
		}))
		since, until := time.Unix(100, 0), time.Unix(200, 0)

		tests := []struct {
			name          string
			q             store.RecentQuery
			want          []string
			wantTruncated bool
		}{
			{"window, others only", store.RecentQuery{Since: since, Until: until}, []string{"a2", "b1", "a1"}, false},
			{"no until", store.RecentQuery{Since: since}, []string{"late", "a2", "b1", "a1"}, false},
			{"include own", store.RecentQuery{Since: since, Until: until, IncludeFromMe: true}, []string{"a2", "b1", "mine", "a1"}, false},
			{"one chat", store.RecentQuery{Since: since, Until: until, ChatJID: b}, []string{"b1"}, false},
			{"truncated keeps newest", store.RecentQuery{Since: since, Until: until, Limit: 2}, []string{"a2", "b1"}, true},
			{"limit exactly fits", store.RecentQuery{Since: since, Until: until, Limit: 3}, []string{"a2", "b1", "a1"}, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				msgs, truncated, err := s.RecentMessages(ctx, tt.q)
				must(t, err)
				if got := ids(msgs); !slices.Equal(got, tt.want) || truncated != tt.wantTruncated {
					t.Errorf("got %v truncated=%v, want %v truncated=%v", got, truncated, tt.want, tt.wantTruncated)
				}
			})
		}

		chats, err := s.ChatsByJID(ctx, []string{a, b, "unknown@s.whatsapp.net"})
		must(t, err)
		if len(chats) != 2 || chats[a].Name != "Alice" {
			t.Errorf("ChatsByJID = %+v", chats)
		}
	})
}

func TestMessageContext(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const chat, other = "1@s.whatsapp.net", "2@s.whatsapp.net"
		var rows []store.Message
		for i := 1; i <= 10; i++ {
			rows = append(rows, msg(chat, fmt.Sprintf("m%02d", i), int64(i*10), "x"))
		}
		// Two messages in the same second, saved in this order: seq breaks the tie.
		rows = append(rows, msg(chat, "same-a", 55, "x"), msg(chat, "same-b", 55, "x"))
		rows = append(rows, msg(other, "o1", 55, "other chat"))
		must(t, s.Save(ctx, nil, rows))

		tests := []struct {
			name          string
			id            string
			before, after int
			want          []string
			wantTarget    int
		}{
			{"middle", "m05", 2, 2, []string{"m03", "m04", "m05", "same-a", "same-b"}, 2},
			{"same second, ordered by seq", "same-a", 1, 1, []string{"m05", "same-a", "same-b"}, 1},
			{"start of chat", "m01", 5, 1, []string{"m01", "m02"}, 0},
			{"end of chat", "m10", 1, 5, []string{"m09", "m10"}, 1},
			{"nothing before", "m06", 0, 1, []string{"m06", "m07"}, 0},
			{"only the message", "m06", 0, 0, []string{"m06"}, 0},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				msgs, target, err := s.MessageContext(ctx, chat, tt.id, tt.before, tt.after)
				must(t, err)
				if got := ids(msgs); !slices.Equal(got, tt.want) || target != tt.wantTarget {
					t.Errorf("got %v target %d, want %v target %d", got, target, tt.want, tt.wantTarget)
				}
			})
		}

		if _, _, err := s.MessageContext(ctx, chat, "nope", 1, 1); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("unknown message: err = %v, want ErrNotFound", err)
		}
		if _, _, err := s.MessageContext(ctx, other, "m05", 1, 1); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("message from another chat: err = %v, want ErrNotFound", err)
		}
	})
}

func TestOldestMessageAndCountBefore(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const chat = "1@s.whatsapp.net"
		if _, err := s.OldestMessage(ctx, chat); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("empty chat: err = %v, want ErrNotFound", err)
		}
		// first-a and first-b share the oldest second; first-a was archived first.
		must(t, s.Save(ctx, nil, []store.Message{
			msg(chat, "first-a", 10, "x"), msg(chat, "first-b", 10, "x"),
			msg(chat, "m2", 20, "x"), msg(chat, "m3", 30, "x"),
			msg("2@s.whatsapp.net", "elsewhere", 1, "x"),
		}))
		oldest, err := s.OldestMessage(ctx, chat)
		must(t, err)
		if oldest.MessageID != "first-a" {
			t.Errorf("oldest = %s, want first-a", oldest.MessageID)
		}
		for ts, want := range map[int64]int64{10: 0, 11: 2, 30: 3, 31: 4} {
			n, err := s.CountBefore(ctx, chat, ts)
			must(t, err)
			if n != want {
				t.Errorf("CountBefore(%d) = %d, want %d", ts, n, want)
			}
		}
	})
}

func TestEditsAndDeletions(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const chat = "1@s.whatsapp.net"
		must(t, s.Save(ctx, nil, []store.Message{
			msg(chat, "e", 10, "meet at the station"),
			msg(chat, "d", 20, "secret plan"),
			msg(chat, "keep", 30, "unchanged"),
		}))
		searchIDs := func(q string) []string {
			t.Helper()
			found, err := s.SearchMessages(ctx, q, "", 0)
			must(t, err)
			return ids(found)
		}
		applied := func(ok bool, err error) bool {
			t.Helper()
			must(t, err)
			return ok
		}

		// Edit: new text is searchable, the old text is gone.
		if !applied(s.ApplyEdit(ctx, chat, "e", "meet at the harbour", "", nil, 100)) {
			t.Fatal("edit not applied")
		}
		if got := searchIDs("harbour"); !slices.Equal(got, []string{"e"}) {
			t.Errorf("search new text = %v, want [e]", got)
		}
		if got := searchIDs("station"); len(got) != 0 {
			t.Errorf("old text still found: %v", got)
		}
		// A late, older edit must not overwrite the newer one; a replay changes nothing.
		if applied(s.ApplyEdit(ctx, chat, "e", "meet at the station", "", nil, 50)) {
			t.Error("older edit was applied")
		}
		if applied(s.ApplyEdit(ctx, chat, "e", "meet at the harbour", "", nil, 100)) {
			t.Error("replayed edit reported a change")
		}
		if applied(s.ApplyEdit(ctx, chat, "unknown", "x", "", nil, 100)) {
			t.Error("edit of an unarchived message reported a change")
		}

		// Deletion: gone from every tool-facing query, text cleared.
		if !applied(s.MarkDeleted(ctx, chat, "d")) {
			t.Fatal("delete not applied")
		}
		if applied(s.MarkDeleted(ctx, chat, "d")) {
			t.Error("second delete reported a change")
		}
		if applied(s.ApplyEdit(ctx, chat, "d", "resurrected", "", nil, 200)) {
			t.Error("edit after delete was applied")
		}
		listed, err := s.ListMessages(ctx, chat, time.Time{}, 0)
		must(t, err)
		if got := ids(listed); !slices.Equal(got, []string{"keep", "e"}) {
			t.Errorf("list = %v, want [keep e]", got)
		}
		for _, m := range listed {
			if m.MessageID == "e" && (m.Text != "meet at the harbour" || m.EditedAt != 100) {
				t.Errorf("edited message = %+v", m)
			}
			if m.MessageID == "keep" && m.EditedAt != 0 {
				t.Errorf("unedited message has EditedAt %d", m.EditedAt)
			}
		}
		if got := searchIDs("secret"); len(got) != 0 {
			t.Errorf("deleted text still found: %v", got)
		}
		recent, _, err := s.RecentMessages(ctx, store.RecentQuery{Since: time.Unix(0, 0)})
		must(t, err)
		if slices.Contains(ids(recent), "d") {
			t.Error("deleted message in recent messages")
		}
		around, _, err := s.MessageContext(ctx, chat, "e", 5, 5)
		must(t, err)
		if got := ids(around); !slices.Equal(got, []string{"e", "keep"}) {
			t.Errorf("context = %v, want [e keep]", got)
		}
		if _, _, err := s.MessageContext(ctx, chat, "d", 1, 1); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("context of deleted message: err = %v, want ErrNotFound", err)
		}
		// Deleted messages still anchor on-demand history fetches.
		applied(s.MarkDeleted(ctx, chat, "e"))
		applied(s.MarkDeleted(ctx, chat, "keep"))
		if _, err := s.OldestMessage(ctx, chat); err != nil {
			t.Errorf("OldestMessage with only deleted messages: %v", err)
		}
	})
}

// oldMessage is the messages table as it was before edits and deletions
// were tracked, to check that store.New upgrades existing databases.
type oldMessage struct {
	Seq       int64  `gorm:"primaryKey;autoIncrement"`
	ChatJID   string `gorm:"column:chat_jid;not null;uniqueIndex:idx_messages_chat_message"`
	MessageID string `gorm:"not null;uniqueIndex:idx_messages_chat_message"`
	SenderJID string `gorm:"column:sender_jid;not null"`
	Timestamp int64  `gorm:"not null"`
	FromMe    bool   `gorm:"not null"`
	Text      string `gorm:"not null;default:''"`
	MediaType string `gorm:"not null;default:''"`
}

func (oldMessage) TableName() string { return "messages" }

func TestMigrationAddsEditColumns(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open("sqlite://" + filepath.Join(t.TempDir(), "old.db"))
	must(t, err)
	t.Cleanup(func() { _ = d.Close() })
	must(t, d.Gorm.AutoMigrate(&oldMessage{}))
	must(t, d.Gorm.Create(&oldMessage{ChatJID: "1@s.whatsapp.net", MessageID: "m1", SenderJID: "x", Timestamp: 1, Text: "from before"}).Error)

	s, err := store.New(ctx, d)
	must(t, err)
	msgs, err := s.ListMessages(ctx, "1@s.whatsapp.net", time.Time{}, 0)
	must(t, err)
	if len(msgs) != 1 || msgs[0].Deleted || msgs[0].EditedAt != 0 {
		t.Fatalf("after migration: %+v", msgs)
	}
	// Rows from before the search index existed are indexed when it is created.
	found, err := s.SearchMessages(ctx, "before", "", 0)
	must(t, err)
	if len(found) != 1 {
		t.Errorf("pre-existing row not searchable: %+v", found)
	}
	ok, err := s.ApplyEdit(ctx, "1@s.whatsapp.net", "m1", "edited", "", nil, 5)
	must(t, err)
	if !ok {
		t.Error("edit on a migrated row not applied")
	}
	found, err = s.SearchMessages(ctx, "edited", "", 0)
	must(t, err)
	if len(found) != 1 {
		t.Errorf("edited migrated row not searchable: %+v", found)
	}
}

func TestMessageByIDAndLatestIncoming(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const chat = "1@s.whatsapp.net"
		mine := msg(chat, "mine", 40, "from me")
		mine.FromMe = true
		must(t, s.Save(ctx, nil, []store.Message{
			msg(chat, "in1", 10, "a"), msg(chat, "in2", 20, "b"), msg(chat, "gone", 30, "c"), mine,
			msg("2@s.whatsapp.net", "elsewhere", 50, "d"),
		}))
		_, err := s.MarkDeleted(ctx, chat, "gone")
		must(t, err)

		m, err := s.MessageByID(ctx, chat, "in2")
		must(t, err)
		if m.Text != "b" {
			t.Errorf("MessageByID = %+v", m)
		}
		for _, id := range []string{"unknown", "gone", "elsewhere"} {
			if _, err := s.MessageByID(ctx, chat, id); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("MessageByID(%s): err = %v, want ErrNotFound", id, err)
			}
		}

		in, err := s.LatestIncoming(ctx, chat, 0)
		must(t, err)
		if got := ids(in); !slices.Equal(got, []string{"in2", "in1"}) {
			t.Errorf("LatestIncoming = %v, want [in2 in1] (no own, deleted or other-chat messages)", got)
		}
		in, err = s.LatestIncoming(ctx, chat, 1)
		must(t, err)
		if got := ids(in); !slices.Equal(got, []string{"in2"}) {
			t.Errorf("LatestIncoming limit 1 = %v", got)
		}
	})
}

func TestMediaRef(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const chat = "1@s.whatsapp.net"
		withRef := func(id, text string, ref []byte) store.Message {
			m := msg(chat, id, 10, text)
			m.MediaType = "image"
			m.MediaRef = ref
			return m
		}
		must(t, s.Save(ctx, nil, []store.Message{
			withRef("new", "caption", []byte("ref-new")),
			withRef("old", "old caption", nil), // archived before refs existed
			withRef("gone", "deleted", nil),
		}))
		_, err := s.ApplyEdit(ctx, chat, "old", "edited caption", "image", nil, 50)
		must(t, err)
		_, err = s.MarkDeleted(ctx, chat, "gone")
		must(t, err)

		// A replay brings refs for all three, with stale text.
		must(t, s.Save(ctx, nil, []store.Message{
			withRef("new", "caption", []byte("ref-replayed")),
			withRef("old", "old caption", []byte("ref-old")),
			withRef("gone", "deleted", []byte("ref-gone")),
		}))

		get := func(id string) store.Message {
			t.Helper()
			var m store.Message
			msgs, _, err := s.MessageContext(ctx, chat, id, 0, 0)
			if err == nil {
				m = msgs[0]
			}
			return m
		}
		if m := get("new"); string(m.MediaRef) != "ref-new" {
			t.Errorf("existing ref overwritten: %q", m.MediaRef)
		}
		if m := get("old"); string(m.MediaRef) != "ref-old" || m.Text != "edited caption" || m.EditedAt != 50 {
			t.Errorf("backfill: ref %q text %q edited %d; want ref-old, edit kept", m.MediaRef, m.Text, m.EditedAt)
		}
		if _, err := s.MessageByID(ctx, chat, "gone"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("deleted message reachable: %v", err)
		}
		var ref []byte
		must(t, s.RawRef(ctx, chat, "gone", &ref))
		if len(ref) != 0 {
			t.Errorf("deleted message got a media ref back: %q", ref)
		}

		// Deleting clears the ref.
		_, err = s.MarkDeleted(ctx, chat, "new")
		must(t, err)
		must(t, s.RawRef(ctx, chat, "new", &ref))
		if len(ref) != 0 {
			t.Errorf("ref kept after delete: %q", ref)
		}
	})
}

func TestApplyEditReplacesTextRef(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const chat = "1@s.whatsapp.net"
		reply := func(id string) store.Message {
			m := msg(chat, id, 10, "pre-edit secret")
			m.MediaRef = []byte("ref-with-pre-edit-secret")
			return m
		}
		img := msg(chat, "img", 10, "caption")
		img.MediaType, img.MediaRef = "image", []byte("ref-img")
		must(t, s.Save(ctx, nil, []store.Message{reply("remote"), reply("own"), img}))

		// A remote edit leaves no ref, so no pre-edit text survives.
		_, err := s.ApplyEdit(ctx, chat, "remote", "new text", "", nil, 50)
		must(t, err)
		// An own edit swaps in the rebuilt ref.
		_, err = s.ApplyEdit(ctx, chat, "own", "new text", "", []byte("ref-new"), 50)
		must(t, err)
		// A media message keeps the ref its download needs.
		_, err = s.ApplyEdit(ctx, chat, "img", "new caption", "image", nil, 50)
		must(t, err)

		// A replay of the originals brings the pre-edit ref back.
		must(t, s.Save(ctx, nil, []store.Message{reply("remote"), reply("own")}))

		var ref []byte
		for id, want := range map[string]string{"remote": "", "own": "ref-new", "img": "ref-img"} {
			must(t, s.RawRef(ctx, chat, id, &ref))
			if string(ref) != want {
				t.Errorf("%s: ref %q, want %q", id, ref, want)
			}
		}
	})
}

func TestReopenKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wa.db")
	ctx := context.Background()
	for i := range 2 {
		d, err := db.Open("sqlite://" + path)
		must(t, err)
		s, err := store.New(ctx, d)
		must(t, err)
		if i == 0 {
			must(t, s.Save(ctx, nil, []store.Message{msg("1@s.whatsapp.net", "m1", 1, "persisted")}))
		} else {
			got, err := s.SearchMessages(ctx, "persisted", "", 0)
			must(t, err)
			if len(got) != 1 {
				t.Errorf("after reopen: got %d messages, want 1", len(got))
			}
		}
		must(t, d.Close())
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
