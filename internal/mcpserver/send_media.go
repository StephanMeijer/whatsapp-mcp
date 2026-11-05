// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

type SendMediaIn struct {
	Recipient string `json:"recipient" jsonschema:"a JID or an international phone number"`
	Kind      string `json:"kind" jsonschema:"image, video, audio or document"`
	Data      string `json:"data,omitempty" jsonschema:"the file, base64-encoded; or use path"`
	Path      string `json:"path,omitempty" jsonschema:"a file in the server's media folder, relative to it"`
	Caption   string `json:"caption,omitempty" jsonschema:"text shown with an image, video or document"`
	FileName  string `json:"file_name,omitempty" jsonschema:"for documents; defaults to the name of path"`
	MIMEType  string `json:"mime_type,omitempty" jsonschema:"optional; detected from the content otherwise"`
}

func (t *tools) sendMedia(ctx context.Context, _ *mcp.CallToolRequest, in SendMediaIn) (*mcp.CallToolResult, Message, error) {
	if in.Kind == "" {
		return nil, Message{}, errors.New("kind is required: image, video, audio or document")
	}
	to, err := t.policy.checkRecipient(ctx, t.wa, in.Recipient)
	if err != nil {
		return nil, Message{}, err
	}
	var data []byte
	fileName := in.FileName
	switch {
	case in.Data != "" && in.Path != "":
		return nil, Message{}, errors.New("pass either data or path, not both")
	case in.Path != "":
		if data, err = t.readMediaFile(in.Path); err != nil {
			return nil, Message{}, err
		}
		if fileName == "" {
			fileName = filepath.Base(in.Path)
		}
	case in.Data != "":
		if data, err = decodeBase64(in.Data); err != nil {
			return nil, Message{}, err
		}
	default:
		return nil, Message{}, errors.New("the file is required: pass data (base64) or path")
	}
	if len(data) > wa.MaxMediaBytes {
		return nil, Message{}, fmt.Errorf("file is over the %d MB limit", wa.MaxMediaBytes>>20)
	}
	m, err := t.wa.SendMedia(ctx, to.String(), wa.OutgoingMedia{
		Kind: store.MediaKind(in.Kind), Data: data, MIMEType: in.MIMEType, Caption: in.Caption, FileName: fileName,
	})
	if err != nil {
		return nil, Message{}, err
	}
	return nil, t.toMessages(ctx, []store.Message{m})[0], nil
}

// decodeBase64 accepts standard base64, padded or not.
func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if data, err := base64.StdEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	data, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return nil, errors.New("data is not valid base64")
	}
	return data, nil
}

// readMediaFile reads a file from the media folder, refusing anything outside it.
func (t *tools) readMediaFile(path string) ([]byte, error) {
	if t.mediaRoot == nil {
		return nil, errors.New("reading files is disabled on this server (no -media-dir); pass the file as data instead")
	}
	f, err := t.mediaRoot.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %q in the media folder: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, wa.MaxMediaBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	return data, nil
}
