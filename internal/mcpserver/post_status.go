// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

type PostStatusIn struct {
	Text     string `json:"text,omitempty" jsonschema:"a text status; or post a photo or video with data or path"`
	Kind     string `json:"kind,omitempty" jsonschema:"for a file: image or video"`
	Data     string `json:"data,omitempty" jsonschema:"the file, base64-encoded; or use path"`
	Path     string `json:"path,omitempty" jsonschema:"a file in the server's media folder, relative to it"`
	Caption  string `json:"caption,omitempty" jsonschema:"text shown with a photo or video"`
	MIMEType string `json:"mime_type,omitempty" jsonschema:"optional; detected from the content otherwise"`
}

func (t *tools) postStatus(ctx context.Context, _ *mcp.CallToolRequest, in PostStatusIn) (*mcp.CallToolResult, Message, error) {
	// A status goes to the whole status audience, which no allowlist entry covers.
	if t.policy.hasAllowlist() && !t.policy.allowStatus {
		return nil, Message{}, errors.New("posting a status is off while an allowlist is set, because a status " +
			"goes to all your status contacts; start the server with -allow-status to allow it")
	}
	hasFile := in.Data != "" || in.Path != ""
	text := strings.TrimSpace(in.Text)
	switch {
	case text != "" && hasFile:
		return nil, Message{}, errors.New("pass either text or a file, not both; use caption for text with a photo or video")
	case text == "" && !hasFile:
		return nil, Message{}, errors.New("a status needs text, or a photo or video as data or path")
	case text != "":
		m, err := t.wa.PostStatus(ctx, text)
		if err != nil {
			return nil, Message{}, err
		}
		return nil, t.toMessages(ctx, []store.Message{m})[0], nil
	}
	kind := store.MediaKind(in.Kind)
	if kind != store.MediaImage && kind != store.MediaVideo {
		return nil, Message{}, fmt.Errorf("kind: a status file is an image or a video, not %q", in.Kind)
	}
	var data []byte
	var err error
	switch {
	case in.Data != "" && in.Path != "":
		return nil, Message{}, errors.New("pass either data or path, not both")
	case in.Path != "":
		data, err = t.readMediaFile(in.Path)
	default:
		data, err = decodeBase64(in.Data)
	}
	if err != nil {
		return nil, Message{}, err
	}
	if len(data) > wa.MaxMediaBytes {
		return nil, Message{}, fmt.Errorf("file is over the %d MB limit", wa.MaxMediaBytes>>20)
	}
	m, err := t.wa.SendMedia(ctx, types.StatusBroadcastJID.String(), wa.OutgoingMedia{
		Kind: kind, Data: data, MIMEType: in.MIMEType, Caption: in.Caption,
	})
	if err != nil {
		return nil, Message{}, err
	}
	return nil, t.toMessages(ctx, []store.Message{m})[0], nil
}
