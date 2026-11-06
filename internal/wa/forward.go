// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"fmt"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// Forward sends an archived message to recipient labelled "Forwarded", as
// the phone does, and archives it. Text is sent from the archive. A file is
// downloaded and uploaded again, so a file that has expired from WhatsApp's
// servers fails with Media's error instead of reaching the recipient broken.
func (c *Client) Forward(ctx context.Context, recipient string, m store.Message) (store.Message, error) {
	to, err := c.recipient(recipient)
	if err != nil {
		return store.Message{}, err
	}
	if m.Deleted {
		return store.Message{}, errors.New("this message was deleted and can't be forwarded")
	}
	// WhatsApp counts every hop; from 5 the phone says "Forwarded many times".
	ci := &waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(uint32(m.ForwardingScore) + 1)}
	switch m.MediaType {
	case "":
		msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(m.Text), ContextInfo: ci}}
		return c.sendAndArchive(ctx, to, msg, "forward", "from", m.ChatJID, "source_id", m.MessageID)
	case store.MediaImage, store.MediaVideo, store.MediaAudio, store.MediaDocument:
	default:
		// shortcut: stickers, polls, locations and contacts are refused; rebuild them from MediaRef or the text when someone needs it.
		return store.Message{}, fmt.Errorf("a %s can't be forwarded; only text, images, videos, audio and documents can", m.MediaType)
	}
	media, err := c.Media(ctx, m)
	if err != nil {
		return store.Message{}, err
	}
	// The archived text falls back to a document's title or file name, so take the caption from the file's own message.
	sub, _, err := decodeMediaRef(m.MediaType, m.MediaRef)
	if err != nil {
		return store.Message{}, err
	}
	caption := ""
	if cs, ok := sub.(interface{ GetCaption() string }); ok {
		caption = cs.GetCaption()
	}
	// An edit updates the text but keeps the original MediaRef, so the text holds the current caption.
	if m.EditedAt > 0 {
		caption = m.Text
		if doc, ok := sub.(*waE2E.DocumentMessage); ok && (caption == doc.GetTitle() || caption == doc.GetFileName()) {
			caption = "" // the edited text fell back to the title or file name
		}
	}
	up, err := c.cli.Upload(ctx, media.Data, mediaTypeFor(m.MediaType))
	if err != nil {
		return store.Message{}, fmt.Errorf("upload: %w", err)
	}
	msg := buildMediaMessage(m.MediaType, up, media.MIMEType, caption, media.FileName)
	if src, ok := sub.(*waE2E.AudioMessage); ok {
		msg.AudioMessage.PTT = src.PTT // a voice note stays a voice note
	}
	setContextInfo(msg, ci)
	return c.sendAndArchive(ctx, to, msg, "forward", "from", m.ChatJID, "source_id", m.MessageID, "kind", m.MediaType)
}
