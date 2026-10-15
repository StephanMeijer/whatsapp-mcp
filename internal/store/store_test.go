// SPDX-License-Identifier: MPL-2.0

package store_test

import (
	"context"
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
