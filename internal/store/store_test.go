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
		if err := d.Gorm.Exec("DROP TABLE IF EXISTS messages, chats").Error; err != nil {
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

		chats, err := s.ListChats(ctx, "", 0)
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

		chats, err := s.ListChats(ctx, "", 0)
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

		chats, err := s.ListChats(ctx, "", 0)
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
				chats, err := s.ListChats(ctx, tt.query, tt.limit)
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
