// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// TestWriteToolsSurfaceWhatsAppErrors checks that when WhatsApp refuses a
// write, the tool fails with WhatsApp's reason and the archive keeps what it
// had: nothing is recorded as if the write had gone through.
func TestWriteToolsSurfaceWhatsAppErrors(t *testing.T) {
	waErr := errors.New("whatsapp said no")
	tests := []struct {
		tool string
		args map[string]any
	}{
		{"send_message", map[string]any{"recipient": alice, "text": "hi"}},
		{"send_message", map[string]any{"recipient": alice, "text": "hi", "reply_to": "A1"}},
		{"send_media", map[string]any{"recipient": alice, "kind": "document", "file_name": "a.txt", "data": "aGk="}},
		{"react", map[string]any{"chat_jid": alice, "message_id": "A1", "emoji": "👍"}},
		{"edit_message", map[string]any{"chat_jid": alice, "message_id": "MINE", "text": "fixed"}},
		{"delete_message", map[string]any{"chat_jid": alice, "message_id": "MINE"}},
		{"mark_read", map[string]any{"chat_jid": alice}},
		{"set_chat_state", map[string]any{"chat_jid": alice, "archived": true, "pinned": true}},
		{"reject_call", map[string]any{"call_id": "RING"}},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			ctx := context.Background()
			st := interactStore(t)
			if err := st.RecordCallOffer(ctx, store.Call{CallID: "RING", CallerJID: alice, CreatorWire: alice, ChatJID: alice, StartedAt: 1}); err != nil {
				t.Fatal(err)
			}
			fake := &fakeWA{writeErr: waErr}
			cs := connect(t, fake, st, Options{})

			res := call(t, cs, tt.tool, tt.args, nil)
			if !res.IsError {
				t.Fatalf("%s: want tool error, got %v", tt.tool, res.StructuredContent)
			}
			if got := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(got, waErr.Error()) {
				t.Errorf("%s error = %q, want WhatsApp's reason %q", tt.tool, got, waErr)
			}

			chats, err := st.ChatsByJID(ctx, []string{alice})
			if err != nil {
				t.Fatal(err)
			}
			if c := chats[alice]; c.Archived || c.Pinned {
				t.Errorf("%s: chat flags recorded despite the error: %+v", tt.tool, c)
			}
			call, err := st.CallByID(ctx, "RING")
			if err != nil {
				t.Fatal(err)
			}
			if call.Status != store.CallRinging {
				t.Errorf("%s: call status = %s, want still ringing", tt.tool, call.Status)
			}
			mine, err := st.MessageByID(ctx, alice, "MINE")
			if err != nil {
				t.Fatalf("%s: own message gone from archive: %v", tt.tool, err)
			}
			if mine.Text != "oops" || mine.EditedAt != 0 {
				t.Errorf("%s: own message changed: %+v", tt.tool, mine)
			}
		})
	}
}
