// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type changeKind int

const (
	changeEdit changeKind = iota + 1
	changeRevoke
)

// change is an edit or a deletion for everyone of an earlier message.
type change struct {
	kind   changeKind
	chat   string // canonical chat JID, as the original is archived
	target string // ID of the edited or deleted message
	text   string // edits only
	media  string // edits only
	at     int64  // edits only: Unix seconds of the edit
	ref    []byte // edits only: new media_ref, for the account's own text messages
}

// toChange recognises edits and deletions for everyone. Edits arrive
// unwrapped by whatsmeow (IsEdit) as a MESSAGE_EDIT protocol message
// carrying the original's key and the new content; deletions as a REVOKE
// protocol message carrying the original's key.
func toChange(e *events.Message, canon func(types.JID) types.JID) (change, bool) {
	pm := e.Message.GetProtocolMessage()
	if pm == nil || pm.GetKey().GetID() == "" {
		return change{}, false
	}
	ch := change{chat: canon(e.Info.Chat).String(), target: pm.GetKey().GetID()}
	switch pm.GetType() {
	case waE2E.ProtocolMessage_MESSAGE_EDIT:
		ch.kind = changeEdit
		ch.text, ch.media = messageContent(pm.GetEditedMessage())
		if ch.media == "" {
			ch.ref = mediaRef(pm.GetEditedMessage(), e.Info.IsFromMe)
		}
		ch.at = pm.GetTimestampMS() / 1000
		if ch.at == 0 {
			ch.at = e.Info.Timestamp.Unix()
		}
	case waE2E.ProtocolMessage_REVOKE:
		ch.kind = changeRevoke
	default:
		return change{}, false
	}
	return ch, true
}

// changeFor recognises an edit or deletion in e, including edits sent the
// newer way: as a secret-encrypted message that only decrypts with the
// original message's secret, which whatsmeow keeps.
func (c *Client) changeFor(ctx context.Context, e *events.Message, canon func(types.JID) types.JID) (change, bool) {
	if ch, ok := toChange(e, canon); ok {
		return ch, true
	}
	enc := e.Message.GetSecretEncryptedMessage()
	if enc.GetSecretEncType() != waE2E.SecretEncryptedMessage_MESSAGE_EDIT {
		return change{}, false
	}
	decrypted, err := c.cli.DecryptSecretEncryptedMessage(ctx, e)
	if err != nil {
		c.log.Debug("decrypt message edit", "err", err, "id", enc.GetTargetMessageKey().GetID())
		return change{}, false
	}
	return secretEditChange(e, decrypted, canon)
}

// secretEditChange builds the change for a decrypted secret-encrypted edit.
// The plaintext is either the new content itself or a MESSAGE_EDIT protocol
// message wrapping it.
func secretEditChange(e *events.Message, decrypted *waE2E.Message, canon func(types.JID) types.JID) (change, bool) {
	target := e.Message.GetSecretEncryptedMessage().GetTargetMessageKey().GetID()
	if target == "" || decrypted == nil {
		return change{}, false
	}
	content := decrypted
	at := e.Info.Timestamp.Unix()
	if pm := decrypted.GetProtocolMessage(); pm.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT {
		content = pm.GetEditedMessage()
		if ms := pm.GetTimestampMS(); ms > 0 {
			at = ms / 1000
		}
	}
	text, media := messageContent(content)
	if text == "" && media == "" {
		return change{}, false
	}
	ch := change{kind: changeEdit, chat: canon(e.Info.Chat).String(), target: target, text: text, media: media, at: at}
	if media == "" {
		ch.ref = mediaRef(content, e.Info.IsFromMe)
	}
	return ch, true
}

// applyChange writes a change to the archive. Changes to messages that were
// never archived are ignored.
func (c *Client) applyChange(ctx context.Context, ch change) {
	var applied bool
	var err error
	switch ch.kind {
	case changeEdit:
		applied, err = c.store.ApplyEdit(ctx, ch.chat, ch.target, ch.text, ch.media, ch.ref, ch.at)
	case changeRevoke:
		applied, err = c.store.MarkDeleted(ctx, ch.chat, ch.target)
	}
	if err != nil {
		c.log.Error("apply message change", "err", err, "chat", ch.chat, "id", ch.target)
		return
	}
	kind := map[changeKind]string{changeEdit: "edit", changeRevoke: "deletion"}[ch.kind]
	if !applied {
		c.log.Debug("message change not applied: original not archived, or already newer", "kind", kind, "chat", ch.chat, "id", ch.target)
		return
	}
	c.log.Info("applied message "+kind, "chat", ch.chat, "id", ch.target)
}
