// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// OutgoingMedia is a file to send.
type OutgoingMedia struct {
	Kind     string // image, video, audio or document
	Data     []byte
	MIMEType string // optional; detected from Data when empty
	Caption  string // not shown for audio
	FileName string // documents
}

// allowedMIME lists what WhatsApp accepts per kind; documents take anything.
var allowedMIME = map[string][]string{
	"image": {"image/jpeg", "image/png"},
	"video": {"video/mp4"},
	"audio": {"audio/ogg", "audio/mpeg", "audio/mp4", "audio/aac"},
}

// SendMedia uploads a file, sends it and archives the message with its media
// reference, so get_media can retrieve it again.
func (c *Client) SendMedia(ctx context.Context, recipient string, f OutgoingMedia) (store.Message, error) {
	if !c.cli.IsLoggedIn() {
		return store.Message{}, errors.New("not logged in to WhatsApp")
	}
	to, err := ParseRecipient(recipient)
	if err != nil {
		return store.Message{}, err
	}
	if len(f.Data) == 0 {
		return store.Message{}, errors.New("the file is empty")
	}
	if len(f.Data) > MaxMediaBytes {
		return store.Message{}, fmt.Errorf("file is %d MB, over the %d MB limit", len(f.Data)>>20, MaxMediaBytes>>20)
	}
	mime, err := detectMIME(f.Kind, f.Data, f.MIMEType)
	if err != nil {
		return store.Message{}, err
	}

	up, err := c.cli.Upload(ctx, f.Data, mediaTypeFor(f.Kind))
	if err != nil {
		return store.Message{}, fmt.Errorf("upload: %w", err)
	}
	msg := buildMediaMessage(f.Kind, up, mime, f.Caption, f.FileName)
	resp, err := c.cli.SendMessage(ctx, to, msg)
	if err != nil {
		return store.Message{}, fmt.Errorf("send: %w", err)
	}
	c.log.Info("sent media", "recipient", to.String(), "id", resp.ID, "kind", f.Kind, "bytes", len(f.Data))

	text, kind := messageContent(msg)
	m := store.Message{
		ChatJID:   to.String(),
		MessageID: resp.ID,
		SenderJID: c.cli.Store.ID.ToNonAD().String(),
		Timestamp: resp.Timestamp.Unix(),
		FromMe:    true,
		Text:      text,
		MediaType: kind,
		MediaRef:  mediaRef(msg),
	}
	if err := c.store.Save(ctx, nil, []store.Message{m}); err != nil {
		c.log.Error("archive sent media", "err", err, "id", resp.ID)
	}
	return m, nil
}

// detectMIME settles the MIME type of a file to send: the hint if given,
// otherwise sniffed from the content, then checked against what WhatsApp
// accepts for the kind.
func detectMIME(kind string, data []byte, hint string) (string, error) {
	if _, known := allowedMIME[kind]; !known && kind != "document" {
		return "", fmt.Errorf("kind must be image, video, audio or document, not %q", kind)
	}
	mime := hint
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	mime = strings.ToLower(strings.TrimSpace(strings.SplitN(mime, ";", 2)[0]))
	if kind == "audio" {
		// Sniffing names the containers, not their use as audio.
		switch mime {
		case "application/ogg":
			mime = "audio/ogg"
		case "video/mp4":
			mime = "audio/mp4"
		}
	}
	if allowed, limited := allowedMIME[kind]; limited && !slices.Contains(allowed, mime) {
		return "", fmt.Errorf("a %s must be one of %s, but this file is %s", kind, strings.Join(allowed, ", "), mime)
	}
	return mime, nil
}

func mediaTypeFor(kind string) whatsmeow.MediaType {
	switch kind {
	case "image":
		return whatsmeow.MediaImage
	case "video":
		return whatsmeow.MediaVideo
	case "audio":
		return whatsmeow.MediaAudio
	default:
		return whatsmeow.MediaDocument
	}
}

// buildMediaMessage wraps an uploaded file in the message for its kind.
func buildMediaMessage(kind string, up whatsmeow.UploadResponse, mime, caption, fileName string) *waE2E.Message {
	opt := func(s string) *string {
		if s == "" {
			return nil
		}
		return proto.String(s)
	}
	switch kind {
	case "image":
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String(mime), Caption: opt(caption),
		}}
	case "video":
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String(mime), Caption: opt(caption),
		}}
	case "audio":
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String(mime),
		}}
	default:
		return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String(mime), Caption: opt(caption), FileName: opt(fileName), Title: opt(fileName),
		}}
	}
}
