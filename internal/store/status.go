// SPDX-License-Identifier: MPL-2.0

package store

import (
	"context"
	"time"
)

// StatusChat is the chat WhatsApp delivers status updates (stories) in.
// It is not a conversation, so chat listings, recent messages and search
// leave it out; StatusUpdates reads it.
const StatusChat = "status@broadcast"

// StatusUpdates returns status updates posted at or after since, newest
// first, from one author when author is set.
func (s *Store) StatusUpdates(ctx context.Context, since time.Time, author string, limit int) ([]Message, error) {
	tx := s.db.WithContext(ctx).
		Where("chat_jid = ? AND timestamp >= ? AND deleted = ?", StatusChat, since.Unix(), false)
	if author != "" {
		tx = tx.Where("sender_jid = ?", author)
	}
	var msgs []Message
	err := tx.Order("timestamp DESC").Order("seq DESC").Limit(clampLimit(limit)).Find(&msgs).Error
	return msgs, err
}
