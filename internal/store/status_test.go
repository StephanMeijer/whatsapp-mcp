// SPDX-License-Identifier: MPL-2.0

package store_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestStatusUpdatesAreNotAChat(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		const alice, bob = "31600000001@s.whatsapp.net", "31600000002@s.whatsapp.net"
		status := func(id, from string, ts int64, text string) store.Message {
			return store.Message{ChatJID: store.StatusChat, MessageID: id, SenderJID: from, Timestamp: ts, Text: text}
		}
		must(t, s.Save(ctx, []store.Chat{{JID: store.StatusChat, Name: "Bob"}}, []store.Message{
			status("S1", alice, 100, "sunset at the beach"),
			status("S2", bob, 200, "beach volleyball"),
			status("S3", alice, 300, "back home"),
			status("OLD", alice, 10, "ancient beach"),
			msg(alice, "M1", 150, "see you at the beach"),
		}))
		_, err := s.MarkDeleted(ctx, store.StatusChat, "S3")
		must(t, err)

		chats, err := s.ListChats(ctx, "", store.IncludeArchived, 0)
		must(t, err)
		for _, c := range chats {
			if c.JID == store.StatusChat {
				t.Errorf("ListChats lists the status chat")
			}
		}
		recent, _, err := s.RecentMessages(ctx, store.RecentQuery{Since: time.Unix(0, 0)})
		must(t, err)
		if got := ids(recent); !slices.Equal(got, []string{"M1"}) {
			t.Errorf("RecentMessages = %v, want only the chat message", got)
		}
		found, err := s.SearchMessages(ctx, "beach", "", 0)
		must(t, err)
		if got := ids(found); !slices.Equal(got, []string{"M1"}) {
			t.Errorf("SearchMessages = %v, want only the chat message", got)
		}
		// Asked for explicitly, the status chat is still searchable and readable.
		found, err = s.SearchMessages(ctx, "beach", store.StatusChat, 0)
		must(t, err)
		if len(found) != 3 {
			t.Errorf("SearchMessages in the status chat = %v, want 3", ids(found))
		}
		recent, _, err = s.RecentMessages(ctx, store.RecentQuery{Since: time.Unix(0, 0), ChatJID: store.StatusChat})
		must(t, err)
		if len(recent) != 3 {
			t.Errorf("RecentMessages in the status chat = %v, want 3", ids(recent))
		}

		updates, err := s.StatusUpdates(ctx, time.Unix(50, 0), "", 0)
		must(t, err)
		if got := ids(updates); !slices.Equal(got, []string{"S2", "S1"}) {
			t.Errorf("StatusUpdates = %v, want [S2 S1] (newest first, no deleted or older ones)", got)
		}
		updates, err = s.StatusUpdates(ctx, time.Unix(0, 0), alice, 0)
		must(t, err)
		if got := ids(updates); !slices.Equal(got, []string{"S1", "OLD"}) {
			t.Errorf("StatusUpdates from Alice = %v, want [S1 OLD]", got)
		}
	})
}
