// SPDX-License-Identifier: MPL-2.0

package store

import (
	"context"
	"fmt"
	"slices"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Message statuses, in the order a sent message moves through them.
const (
	StatusSent      = "sent"
	StatusDelivered = "delivered"
	StatusRead      = "read"
	StatusPlayed    = "played" // voice notes and videos that were played
)

var statusOrder = []string{StatusSent, StatusDelivered, StatusRead, StatusPlayed}

// statusesBelow lists the statuses a message may be in and still move to s.
func statusesBelow(s string) []string {
	i := slices.Index(statusOrder, s)
	if i < 0 {
		return nil
	}
	return statusOrder[:i]
}

// Receipt is what one recipient did with one of the account's messages.
// Times are Unix seconds; 0 means it has not happened (or was not reported:
// people who turned off read receipts only ever deliver).
type Receipt struct {
	ChatJID   string `gorm:"column:chat_jid;primaryKey"`
	MessageID string `gorm:"primaryKey"`
	// ParticipantJID is canonical (see Canonical); in a direct chat it is the chat.
	ParticipantJID string `gorm:"column:participant_jid;primaryKey"`
	DeliveredAt    int64  `gorm:"not null;default:0"`
	ReadAt         int64  `gorm:"not null;default:0"`
	PlayedAt       int64  `gorm:"not null;default:0"`
}

// RecordReceipt notes that participant reached status for one of the
// account's own messages at time at, and moves the message's status forward
// if this is further than it was. Receipts for messages that aren't archived,
// or weren't sent by the account, are ignored. A time already recorded is
// kept, so replayed or out-of-order receipts change nothing. It reports
// whether the message's status moved.
func (s *Store) RecordReceipt(ctx context.Context, chatJID, messageID, participant, status string, at time.Time) (bool, error) {
	var column string
	switch status {
	case StatusDelivered:
		column = "delivered_at"
	case StatusRead:
		column = "read_at"
	case StatusPlayed:
		column = "played_at"
	default:
		return false, fmt.Errorf("receipt status %q is not delivered, read or played", status)
	}
	var moved bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&Message{}).Where("chat_jid = ? AND message_id = ? AND from_me = ?", chatJID, messageID, true).
			Count(&n).Error; err != nil || n == 0 {
			return err
		}
		r := Receipt{ChatJID: chatJID, MessageID: messageID, ParticipantJID: participant}
		switch column {
		case "delivered_at":
			r.DeliveredAt = at.Unix()
		case "read_at":
			r.ReadAt = at.Unix()
		case "played_at":
			r.PlayedAt = at.Unix()
		}
		keep := fmt.Sprintf("CASE WHEN receipts.%s = 0 THEN excluded.%s ELSE receipts.%s END", column, column, column)
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "chat_jid"}, {Name: "message_id"}, {Name: "participant_jid"}},
			DoUpdates: clause.Assignments(map[string]any{column: gorm.Expr(keep)}),
		}).Create(&r).Error; err != nil {
			return err
		}
		res := tx.Model(&Message{}).
			Where("chat_jid = ? AND message_id = ? AND from_me = ? AND status IN ?", chatJID, messageID, true, statusesBelow(status)).
			Update("status", status)
		moved = res.RowsAffected > 0
		return res.Error
	})
	return moved, err
}

// Receipts returns what each recipient did with a message, by participant.
func (s *Store) Receipts(ctx context.Context, chatJID, messageID string) ([]Receipt, error) {
	var out []Receipt
	err := s.db.WithContext(ctx).Where("chat_jid = ? AND message_id = ?", chatJID, messageID).
		Order("participant_jid").Find(&out).Error
	return out, err
}
