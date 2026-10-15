// SPDX-License-Identifier: MPL-2.0

package store

// Chat is a conversation: a direct chat, group or broadcast list.
type Chat struct {
	JID  string `gorm:"column:jid;primaryKey"`
	Name string `gorm:"not null;default:''"`
	// LastMessageAt is a Unix timestamp in seconds, the resolution WhatsApp uses.
	LastMessageAt int64 `gorm:"not null;default:0;index"`
}

// Message is one archived message. Only messages with text or media are
// stored; reactions, receipts and protocol messages are not.
type Message struct {
	// Seq is a stable integer key, which SQLite's external-content FTS5 index
	// needs (implicit rowids may be renumbered by VACUUM).
	Seq       int64  `gorm:"primaryKey;autoIncrement"`
	ChatJID   string `gorm:"column:chat_jid;not null;uniqueIndex:idx_messages_chat_message;index:idx_messages_chat_ts,priority:1"`
	MessageID string `gorm:"not null;uniqueIndex:idx_messages_chat_message"`
	SenderJID string `gorm:"column:sender_jid;not null"`
	// Timestamp is a Unix timestamp in seconds.
	Timestamp int64  `gorm:"not null;index:idx_messages_chat_ts,priority:2"`
	FromMe    bool   `gorm:"not null"`
	Text      string `gorm:"not null;default:''"`
	// MediaType is empty for plain text, otherwise image, video, audio, document or sticker.
	MediaType string `gorm:"not null;default:''"`
}
