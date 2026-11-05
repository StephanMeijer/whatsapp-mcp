// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestEditMessageCaptions(t *testing.T) {
	marshal := func(m proto.Message) []byte {
		b, err := proto.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	tests := []struct {
		name    string
		kind    store.MediaKind
		ref     []byte
		caption func(*waE2E.Message) string // nil: the edit must fail
	}{
		{"video", store.MediaVideo, marshal(&waE2E.VideoMessage{Caption: proto.String("old")}),
			func(m *waE2E.Message) string { return m.GetVideoMessage().GetCaption() }},
		{"document", store.MediaDocument, marshal(&waE2E.DocumentMessage{FileName: proto.String("a.pdf")}),
			func(m *waE2E.Message) string { return m.GetDocumentMessage().GetCaption() }},
		{"corrupt image ref", store.MediaImage, []byte{0xff}, nil},
		{"corrupt text ref", "", []byte{0xff}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, fake, _, _ := newTestClient(t)
			mine := store.Message{ChatJID: alicePN.String(), MessageID: "O1", SenderJID: ownJID.ToNonAD().String(),
				Timestamp: time.Now().Add(-time.Minute).Unix(), FromMe: true, Text: "old",
				MediaType: tt.kind, MediaRef: tt.ref}
			archive(t, c, mine)

			_, err := c.EditMessage(context.Background(), alicePN, mine, "new")
			if tt.caption == nil {
				if err == nil {
					t.Fatal("want an error")
				}
				if len(fake.sent) != 0 {
					t.Errorf("sent %d messages, want none", len(fake.sent))
				}
				if got := archived(t, c, alicePN, "O1"); got.Text != "old" || got.EditedAt != 0 {
					t.Errorf("archive changed by a failed edit: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			sent := fake.sent[0].msg.GetEditedMessage().GetMessage().GetProtocolMessage().GetEditedMessage()
			if got := tt.caption(sent); got != "new" {
				t.Errorf("sent caption %q, want %q", got, "new")
			}
			if got := archived(t, c, alicePN, "O1"); got.Text != "new" || got.MediaType != tt.kind {
				t.Errorf("archived %+v", got)
			}
		})
	}
}
