// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// MaxMediaBytes is the largest file Media downloads.
const MaxMediaBytes = 16 << 20

// ErrNoMediaRef means a media message was archived without what is needed
// to download it, before downloads were supported.
var ErrNoMediaRef = errors.New("this media was archived before downloads were supported, so it cannot " +
	"be downloaded unless WhatsApp sends the message again (for example in a later history sync)")

// Media is a downloaded, decrypted media file.
type Media struct {
	Kind     string // image, video, audio, document or sticker
	MIMEType string
	FileName string // documents only
	Data     []byte
}

// downloadable is what every archived media sub-message provides.
type downloadable interface {
	whatsmeow.DownloadableMessage
	GetMimetype() string
	GetFileLength() uint64
}

// Media downloads and decrypts an archived media message's file.
func (c *Client) Media(ctx context.Context, m store.Message) (Media, error) {
	if m.MediaType == "" {
		return Media{}, errors.New("this message has no media")
	}
	if len(m.MediaRef) == 0 {
		return Media{}, ErrNoMediaRef
	}
	sub, fileName, err := decodeMediaRef(m.MediaType, m.MediaRef)
	if err != nil {
		return Media{}, err
	}
	if size := sub.GetFileLength(); size > MaxMediaBytes {
		return Media{}, fmt.Errorf("file is %d MB, over the %d MB limit", size>>20, MaxMediaBytes>>20)
	}
	data, err := c.cli.Download(ctx, sub)
	if mediaGone(err) {
		return Media{}, errors.New("this media is no longer on WhatsApp's servers (they keep files for a limited time)")
	}
	if err != nil {
		return Media{}, fmt.Errorf("download media: %w", err)
	}
	return Media{Kind: m.MediaType, MIMEType: sub.GetMimetype(), FileName: fileName, Data: data}, nil
}

// mediaGone reports whether a download failed because WhatsApp no longer
// serves the file; whatsmeow ends its retries on these statuses too.
func mediaGone(err error) bool {
	return errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith403) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410)
}

// mediaRef marshals a message's media sub-message, which holds what is
// needed to download it later; nil for messages without media. The cases
// mirror messageContent, so the ref matches the archived MediaType.
func mediaRef(m *waE2E.Message) []byte {
	var sub proto.Message
	switch {
	case m.GetImageMessage() != nil:
		sub = m.GetImageMessage()
	case m.GetVideoMessage() != nil:
		sub = m.GetVideoMessage()
	case m.GetAudioMessage() != nil:
		sub = m.GetAudioMessage()
	case m.GetDocumentMessage() != nil:
		sub = m.GetDocumentMessage()
	case m.GetStickerMessage() != nil:
		sub = m.GetStickerMessage()
	default:
		return nil
	}
	ref, err := proto.Marshal(sub)
	if err != nil {
		return nil
	}
	return ref
}

// decodeMediaRef restores the media sub-message saved by mediaRef.
func decodeMediaRef(kind string, ref []byte) (downloadable, string, error) {
	var sub interface {
		downloadable
		proto.Message
	}
	switch kind {
	case "image":
		sub = &waE2E.ImageMessage{}
	case "video":
		sub = &waE2E.VideoMessage{}
	case "audio":
		sub = &waE2E.AudioMessage{}
	case "document":
		sub = &waE2E.DocumentMessage{}
	case "sticker":
		sub = &waE2E.StickerMessage{}
	default:
		return nil, "", fmt.Errorf("unknown media type %q", kind)
	}
	if err := proto.Unmarshal(ref, sub); err != nil {
		return nil, "", fmt.Errorf("decode media reference: %w", err)
	}
	fileName := ""
	if doc, ok := sub.(*waE2E.DocumentMessage); ok {
		fileName = doc.GetFileName()
	}
	return sub, fileName, nil
}
