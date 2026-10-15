// SPDX-License-Identifier: MPL-2.0

package store

import "context"

// RawRef reads a message's media reference regardless of its deleted state,
// for tests that check what is left in the database.
func (s *Store) RawRef(ctx context.Context, chatJID, messageID string, out *[]byte) error {
	var m Message
	err := s.db.WithContext(ctx).Where("chat_jid = ? AND message_id = ?", chatJID, messageID).Limit(1).Find(&m).Error
	*out = m.MediaRef
	return err
}
