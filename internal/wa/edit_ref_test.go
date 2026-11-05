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
		name string
		kind store.MediaKind
		ref  []byte
		// fields returns the sent caption and one original media field the edit must keep; nil: the edit must fail.
		fields func(*waE2E.Message) (caption, kept string)
		kept   string
	}{
		{"video", store.MediaVideo, marshal(&waE2E.VideoMessage{Caption: proto.String("old"), Mimetype: proto.String("video/mp4")}),
			func(m *waE2E.Message) (string, string) {
				return m.GetVideoMessage().GetCaption(), m.GetVideoMessage().GetMimetype()
			}, "video/mp4"},
		{"document", store.MediaDocument, marshal(&waE2E.DocumentMessage{FileName: proto.String("a.pdf")}),
			func(m *waE2E.Message) (string, string) {
				return m.GetDocumentMessage().GetCaption(), m.GetDocumentMessage().GetFileName()
			}, "a.pdf"},
		{"corrupt image ref", store.MediaImage, []byte{0xff}, nil, ""},
		{"corrupt text ref", "", []byte{0xff}, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, fake, _, _ := newTestClient(t)
			mine := store.Message{ChatJID: alicePN.String(), MessageID: "O1", SenderJID: ownJID.ToNonAD().String(),
				Timestamp: time.Now().Add(-time.Minute).Unix(), FromMe: true, Text: "old",
				MediaType: tt.kind, MediaRef: tt.ref}
			archive(t, c, mine)

			_, err := c.EditMessage(context.Background(), alicePN, mine, "new")
			if tt.fields == nil {
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
			if len(fake.sent) != 1 {
				t.Fatalf("sent %d messages, want 1", len(fake.sent))
			}
			sent := fake.sent[0].msg.GetEditedMessage().GetMessage().GetProtocolMessage().GetEditedMessage()
			caption, kept := tt.fields(sent)
			if caption != "new" {
				t.Errorf("sent caption %q, want %q", caption, "new")
			}
			if kept != tt.kept {
				t.Errorf("sent media field %q, want original %q", kept, tt.kept)
			}
			if got := archived(t, c, alicePN, "O1"); got.Text != "new" || got.MediaType != tt.kind {
				t.Errorf("archived %+v", got)
			}
		})
	}
}
