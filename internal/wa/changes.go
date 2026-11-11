// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type changeKind int

const (
	changeEdit changeKind = iota + 1
	changeRevoke
	changeKeep
)

var changeNames = map[changeKind]string{changeEdit: "edit", changeRevoke: "deletion", changeKeep: "keep"}

// change is an edit, a deletion for everyone, or a "keep in chat" of an
// earlier message.
type change struct {
	kind   changeKind
	chat   string          // canonical chat JID, as the original is archived
	target string          // ID of the edited or deleted message
	text   string          // edits only
	media  store.MediaKind // edits only
	at     int64           // edits only: Unix seconds of the edit
	ref    []byte          // edits only: new media_ref, for the account's own text messages
}

// toChange recognises edits and deletions for everyone. Edits arrive
// unwrapped by whatsmeow (IsEdit) as a MESSAGE_EDIT protocol message
// carrying the original's key and the new content; deletions as a REVOKE
// protocol message carrying the original's key. A "keep in chat" for
// everyone stops the original from disappearing.
func toChange(e *events.Message, canon func(types.JID) types.JID) (change, bool) {
	// shortcut: UNDO_KEEP_FOR_ALL is ignored, so an unkept message never expires here; restore its expiry if archives must shrink after an undo.
	if k := e.Message.GetKeepInChatMessage(); k.GetKeepType() == waE2E.KeepType_KEEP_FOR_ALL && k.GetKey().GetID() != "" {
		return change{kind: changeKeep, chat: canon(e.Info.Chat).String(), target: k.GetKey().GetID()}, true
	}
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
	case changeKeep:
		applied, err = c.store.KeepMessage(ctx, ch.chat, ch.target)
	}
	if err != nil {
		c.log.Error("apply message change", "err", err, "chat", ch.chat, "id", ch.target)
		return
	}
	kind := changeNames[ch.kind]
	if !applied {
		c.log.Debug("message change not applied: original not archived, or already newer", "kind", kind, "chat", ch.chat, "id", ch.target)
		return
	}
	c.log.Info("applied message "+kind, "chat", ch.chat, "id", ch.target)
}

// mayChange reports whether the sender of the live message e may make the
// edit or deletion ch: only a message's author may edit it, and only its
// author or a group admin may delete it. WhatsApp checks neither on receipt,
// so without this anyone in a chat could rewrite what others said in the
// archive. The account's own devices are trusted: the phone already checked.
func (c *Client) mayChange(ctx context.Context, e *events.Message, ch change, canon func(types.JID) types.JID) bool {
	if ch.kind == changeKeep {
		return true
	}
	target, err := c.store.MessageByID(ctx, ch.chat, ch.target)
	if errors.Is(err, store.ErrNotFound) {
		return true // nothing to change; applyChange says so
	}
	if err != nil {
		c.log.Error("look up target of message change", "err", err, "chat", ch.chat, "id", ch.target)
		return false
	}
	sender := canon(e.Info.Sender)
	switch {
	case e.Info.IsFromMe:
		// Own messages are matched by from_me: the sender may be the own LID.
		if ch.kind == changeRevoke || target.FromMe {
			return true
		}
	case sameUser(sender, target.SenderJID, canon):
		return true
	// Not e.Info.IsGroup: whatsmeow sets it for status@broadcast too, which has no admins.
	case ch.kind == changeRevoke && e.Info.Chat.Server == types.GroupServer && c.isGroupAdmin(ctx, e.Info.Chat, sender, canon):
		return true
	}
	c.log.Warn("ignored "+changeNames[ch.kind]+" from someone other than the author",
		"chat", ch.chat, "id", ch.target, "sender", sender.String())
	return false
}

// sameUser reports whether sender is the archived author, comparing
// canonical forms: the author may be archived under an @lid whose phone
// number became known later.
func sameUser(sender types.JID, author string, canon func(types.JID) types.JID) bool {
	jid, err := types.ParseJID(author)
	return err == nil && canon(jid) == sender
}

// isGroupAdmin asks WhatsApp whether user is an admin of group.
func (c *Client) isGroupAdmin(ctx context.Context, group, user types.JID, canon func(types.JID) types.JID) bool {
	info, err := c.cli.GetGroupInfo(ctx, group.ToNonAD())
	if err != nil {
		c.log.Warn("look up group admins", "err", err, "group", group.String())
		return false
	}
	for _, p := range info.Participants {
		if (p.IsAdmin || p.IsSuperAdmin) && canon(participantJID(p)) == user {
			return true
		}
	}
	return false
}
