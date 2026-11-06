// SPDX-License-Identifier: MPL-2.0

package store

// Chat is a conversation: a direct chat, group or broadcast list.
type Chat struct {
	JID  string `gorm:"column:jid;primaryKey"`
	Name string `gorm:"not null;default:''"`
	// LastMessageAt is a Unix timestamp in seconds, the resolution WhatsApp uses.
	LastMessageAt int64 `gorm:"not null;default:0;index"`
	// Archived, Pinned and MutedUntil mirror the phone's app state (see
	// SetChatFlags). MutedUntil is Unix seconds; 0 is not muted, -1 forever.
	Archived   bool  `gorm:"not null;default:false"`
	Pinned     bool  `gorm:"not null;default:false"`
	MutedUntil int64 `gorm:"not null;default:0"`
	// DisappearingSeconds is the chat's disappearing-message timer, 0 when off.
	DisappearingSeconds int64 `gorm:"not null;default:0"`
}

// MediaKind is the kind of media a message carries.
type MediaKind string

const (
	MediaImage    MediaKind = "image"
	MediaVideo    MediaKind = "video"
	MediaAudio    MediaKind = "audio"
	MediaDocument MediaKind = "document"
	MediaSticker  MediaKind = "sticker"
	MediaPoll     MediaKind = "poll"
	MediaLocation MediaKind = "location"
	MediaContact  MediaKind = "contact"
)

// Message is one archived message. Only messages with text or media are
// stored; reactions, receipts and protocol messages are not.
type Message struct {
	// Seq is a stable integer key, which SQLite's external-content FTS5 index
	// needs (implicit rowids may be renumbered by VACUUM).
	Seq       int64  `gorm:"primaryKey;autoIncrement"`
	ChatJID   string `gorm:"column:chat_jid;not null;uniqueIndex:idx_messages_chat_message;index:idx_messages_chat_ts,priority:1"`
	MessageID string `gorm:"not null;uniqueIndex:idx_messages_chat_message"`
	SenderJID string `gorm:"column:sender_jid;not null;index:idx_messages_sender_jid"`
	// Timestamp is a Unix timestamp in seconds.
	Timestamp int64  `gorm:"not null;index:idx_messages_chat_ts,priority:2;index:idx_messages_ts"`
	FromMe    bool   `gorm:"not null"`
	Text      string `gorm:"not null;default:''"`
	// MediaType is empty for plain text, otherwise image, video, audio,
	// document, sticker, poll, location or contact. A poll's Text is its
	// question followed by one "- option" line per option; a location's is
	// its name and address, then its coordinates and a map link; a contact's
	// is one "Name: +number, ..." line per contact card.
	MediaType MediaKind `gorm:"not null;default:''"`
	// EditedAt is the Unix time of the last applied edit; 0 if never edited.
	EditedAt int64 `gorm:"not null;default:0"`
	// Status is how far one of the account's own messages got: sent,
	// delivered, read or played, the furthest any recipient reached. It only
	// moves forward (see RecordReceipt). Incoming messages stay at sent.
	Status string `gorm:"not null;default:'sent'"`
	// Forwarded is set for a message forwarded from another chat;
	// ForwardingScore is how many times it has been forwarded along the way
	// (the phone says "Forwarded many times" from 5).
	Forwarded       bool `gorm:"not null;default:false"`
	ForwardingScore int  `gorm:"not null;default:0"`
	// ExpiresAt is when a message from a disappearing chat vanishes for the
	// people in it, in Unix seconds; 0 for messages that don't disappear.
	// ExpireMessages honours it when the operator opts in.
	ExpiresAt int64 `gorm:"not null;default:0;index"`
	// Deleted marks a message deleted for everyone. Its text is cleared and
	// it is left out of every query that feeds the tools.
	Deleted bool `gorm:"not null;default:false"`
	// MediaRef is the marshalled media sub-message (e.g. an ImageMessage)
	// holding what is needed to download and decrypt the file; nil for plain
	// text and for media archived before downloads were supported. For a
	// reply or link preview the account sent it is the marshalled
	// ExtendedTextMessage, which keeps the quote for edits. For a poll it is
	// the marshalled PollCreationMessage, which holds the option names that
	// votes refer to by hash.
	MediaRef []byte
}
