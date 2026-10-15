// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

func mediaStore(t *testing.T) *store.Store {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	if err := st.Save(ctx, nil, []store.Message{
		{ChatJID: alice, MessageID: "IMG", SenderJID: alice, Timestamp: 1, MediaType: "image", Text: "beach"},
		{ChatJID: alice, MessageID: "DOC", SenderJID: alice, Timestamp: 2, MediaType: "document", Text: "invoice.pdf"},
		{ChatJID: alice, MessageID: "TXT", SenderJID: alice, Timestamp: 3, Text: "plain text"},
		{ChatJID: alice, MessageID: "GONE", SenderJID: alice, Timestamp: 4, MediaType: "image"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkDeleted(ctx, alice, "GONE"); err != nil {
		t.Fatal(err)
	}
	return st
}

func callMedia(t *testing.T, cs *mcp.ClientSession, id string) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_media", Arguments: map[string]any{"chat_jid": alice, "message_id": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestGetMediaImage(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 'j', 'p', 'g'}
	cs := connect(t, &fakeWA{media: wa.Media{MIMEType: "image/jpeg", Data: jpeg}}, mediaStore(t), Options{})
	res := callMedia(t, cs, "IMG")
	if res.IsError || len(res.Content) != 2 {
		t.Fatalf("result = %+v", res)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(text.Text, "image/jpeg") || !strings.Contains(text.Text, `"beach"`) {
		t.Errorf("description = %+v", res.Content[0])
	}
	img, ok := res.Content[1].(*mcp.ImageContent)
	if !ok || img.MIMEType != "image/jpeg" || !bytes.Equal(img.Data, jpeg) {
		t.Errorf("image = %+v", res.Content[1])
	}
}

func TestGetMediaDocument(t *testing.T) {
	pdf := []byte("%PDF-1.7")
	cs := connect(t, &fakeWA{media: wa.Media{MIMEType: "application/pdf", FileName: "invoice.pdf", Data: pdf}}, mediaStore(t), Options{})
	res := callMedia(t, cs, "DOC")
	if res.IsError || len(res.Content) != 2 {
		t.Fatalf("result = %+v", res)
	}
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, `file name "invoice.pdf"`) {
		t.Errorf("description = %q", text)
	}
	doc, ok := res.Content[1].(*mcp.EmbeddedResource)
	if !ok || doc.Resource.MIMEType != "application/pdf" || !bytes.Equal(doc.Resource.Blob, pdf) ||
		doc.Resource.URI != "whatsapp://media/"+alice+"/DOC" {
		t.Errorf("resource = %+v", res.Content[1])
	}
}

func TestGetMediaErrors(t *testing.T) {
	cs := connect(t, &fakeWA{mediaErr: wa.ErrNoMediaRef}, mediaStore(t), Options{})
	for _, id := range []string{"IMG", "UNKNOWN", "GONE"} {
		if res := callMedia(t, cs, id); !res.IsError {
			t.Errorf("get_media(%s): want tool error", id)
		}
	}
	res := callMedia(t, cs, "IMG")
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "archived before downloads were supported") {
		t.Errorf("missing-ref error = %q", text)
	}
}
