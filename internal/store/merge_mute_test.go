// SPDX-License-Identifier: MPL-2.0

package store_test

import (
	"context"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestMergeKeepsMuteForever(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		forever := int64(-1)
		// Chat archived under its @lid, muted forever on the phone.
		if err := s.SetChatFlags(ctx, "111@lid", store.ChatFlags{MutedUntil: &forever}); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(ctx, nil, []store.Message{{ChatJID: "111@lid", MessageID: "m1", SenderJID: "111@lid", Timestamp: 10}}); err != nil {
			t.Fatal(err)
		}
		// The phone number becomes known and a message arrives under it.
		if err := s.Save(ctx, nil, []store.Message{{ChatJID: "31600000000@s.whatsapp.net", MessageID: "m2", SenderJID: "31600000000@s.whatsapp.net", Timestamp: 20}}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.MergeJIDs(ctx, map[string]string{"111@lid": "31600000000@s.whatsapp.net"}); err != nil {
			t.Fatal(err)
		}
		chats, err := s.ChatsByJID(ctx, []string{"31600000000@s.whatsapp.net"})
		if err != nil {
			t.Fatal(err)
		}
		if got := chats["31600000000@s.whatsapp.net"].MutedUntil; got != -1 {
			t.Errorf("merged chat MutedUntil = %d, want -1 (muted forever)", got)
		}
	})
}
