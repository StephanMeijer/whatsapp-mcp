// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

type GetMediaIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"the chat's JID"`
	MessageID string `json:"message_id" jsonschema:"id of a message with media_type set (not a poll)"`
}

// getMedia returns the file as MCP content rather than structured output,
// so a client can render an image or save a document.
func (t *tools) getMedia(ctx context.Context, _ *mcp.CallToolRequest, in GetMediaIn) (*mcp.CallToolResult, any, error) {
	if in.ChatJID == "" || in.MessageID == "" {
		return nil, nil, errors.New("chat_jid and message_id are required")
	}
	chat := t.canonicalChat(ctx, in.ChatJID)
	m, err := t.store.MessageByID(ctx, chat, in.MessageID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, fmt.Errorf("message %s not found in chat %s", in.MessageID, chat)
	}
	if err != nil {
		return nil, nil, err
	}
	media, err := t.wa.Media(ctx, m)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: mediaContent(media, m)}, nil, nil
}

// mediaContent describes a downloaded file in a text line and attaches it:
// images (and stickers) as images, audio as audio, anything else as an
// embedded resource.
func mediaContent(media wa.Media, m store.Message) []mcp.Content {
	desc := fmt.Sprintf("%s, %s, %d bytes", media.Kind, media.MIMEType, len(media.Data))
	if media.FileName != "" {
		desc += ", file name " + strconv.Quote(media.FileName)
	}
	if m.Text != "" {
		desc += ", caption " + strconv.Quote(m.Text)
	}
	out := []mcp.Content{&mcp.TextContent{Text: desc}}
	switch media.Kind {
	case store.MediaImage, store.MediaSticker:
		out = append(out, &mcp.ImageContent{Data: media.Data, MIMEType: media.MIMEType})
	case store.MediaAudio:
		out = append(out, &mcp.AudioContent{Data: media.Data, MIMEType: media.MIMEType})
	default:
		out = append(out, &mcp.EmbeddedResource{Resource: &mcp.ResourceContents{
			URI:      "whatsapp://media/" + m.ChatJID + "/" + m.MessageID,
			MIMEType: media.MIMEType,
			Blob:     media.Data,
		}})
	}
	return out
}
