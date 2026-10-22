// SPDX-License-Identifier: MPL-2.0

package store_test

import (
	"context"
	"slices"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func ptr[T any](v T) *T { return &v }

func TestChatFlags(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const alice, bob, carol = "1@s.whatsapp.net", "2@s.whatsapp.net", "3@s.whatsapp.net"
		must(t, s.Save(ctx, []store.Chat{
			{JID: alice, Name: "Alice", LastMessageAt: 30},
			{JID: bob, Name: "Bob", LastMessageAt: 20},
		}, nil))
		jids := func(archived store.ArchiveFilter) []string {
			t.Helper()
			chats, err := s.ListChats(ctx, "", archived, 0)
			must(t, err)
			out := []string{}
			for _, c := range chats {
				out = append(out, c.JID)
			}
			return out
		}

		// Flags on a chat the archive has never seen create it.
		must(t, s.SetChatFlags(ctx, carol, store.ChatFlags{Archived: ptr(true), MutedUntil: ptr(int64(-1))}))
		must(t, s.SetChatFlags(ctx, bob, store.ChatFlags{Pinned: ptr(true)}))
		must(t, s.SetChatFlags(ctx, alice, store.ChatFlags{})) // nothing to do

		// A later message keeps the flags.
		must(t, s.Save(ctx, nil, []store.Message{{ChatJID: carol, MessageID: "m", SenderJID: carol, Timestamp: 40}}))
		chats, err := s.ChatsByJID(ctx, []string{carol, bob})
		must(t, err)
		if c := chats[carol]; !c.Archived || c.MutedUntil != -1 || c.LastMessageAt != 40 {
			t.Errorf("carol = %+v", c)
		}
		if c := chats[bob]; !c.Pinned || c.Name != "Bob" {
			t.Errorf("bob = %+v", c)
		}

		// Pinned first, archived hidden by default.
		if got := jids(store.ExcludeArchived); !slices.Equal(got, []string{bob, alice}) {
			t.Errorf("exclude archived = %v", got)
		}
		if got := jids(store.IncludeArchived); !slices.Equal(got, []string{bob, carol, alice}) {
			t.Errorf("include archived = %v", got)
		}
		if got := jids(store.OnlyArchived); !slices.Equal(got, []string{carol}) {
			t.Errorf("only archived = %v", got)
		}

		// Unarchiving and unpinning.
		must(t, s.SetChatFlags(ctx, carol, store.ChatFlags{Archived: ptr(false), MutedUntil: ptr(int64(0))}))
		must(t, s.SetChatFlags(ctx, bob, store.ChatFlags{Pinned: ptr(false)}))
		if got := jids(store.ExcludeArchived); !slices.Equal(got, []string{carol, alice, bob}) {
			t.Errorf("after reset = %v", got)
		}

		// Merging an @lid chat keeps flags from either side.
		const lid = "111@lid"
		must(t, s.SetChatFlags(ctx, lid, store.ChatFlags{Archived: ptr(true)}))
		if _, err := s.MergeJIDs(ctx, map[string]string{lid: alice}); err != nil {
			t.Fatal(err)
		}
		chats, err = s.ChatsByJID(ctx, []string{alice})
		must(t, err)
		if !chats[alice].Archived {
			t.Errorf("merged alice = %+v, want archived", chats[alice])
		}
	})
}
