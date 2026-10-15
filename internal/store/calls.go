// SPDX-License-Identifier: MPL-2.0

package store

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Call statuses. A call starts ringing and moves forward only.
const (
	CallRinging  = "ringing"
	CallAnswered = "answered" // accepted on one of the account's devices
	CallRejected = "rejected" // declined, here or on another device
	CallMissed   = "missed"   // ended while still ringing
	CallEnded    = "ended"    // ended after being answered
)

// Call is an incoming call.
type Call struct {
	CallID string `gorm:"column:call_id;primaryKey"`
	// CallerJID is canonical (see Canonical), for names and the allowlist.
	CallerJID string `gorm:"column:caller_jid;not null;default:''"`
	// CreatorWire is the caller as WhatsApp addressed them, which is what a
	// reject must be sent to.
	CreatorWire string `gorm:"not null;default:''"`
	// ChatJID is the group for group calls, otherwise the caller.
	ChatJID   string `gorm:"column:chat_jid;not null;default:''"`
	Video     bool   `gorm:"not null;default:false"`
	Group     bool   `gorm:"column:is_group;not null;default:false"`
	StartedAt int64  `gorm:"not null;default:0;index"`
	EndedAt   int64  `gorm:"not null;default:0"`
	Status    string `gorm:"not null;default:'ringing'"`
	Reason    string `gorm:"not null;default:''"`
}

// CallEvent is what happened to a ringing or answered call.
type CallEvent int

const (
	CallEventAnswered CallEvent = iota + 1
	CallEventRejected
	CallEventTerminated
)

// RecordCallOffer stores a ringing call. A repeated offer for the same call
// (an offer and its notice both arrive) only adds detail: video and group
// flags are never cleared, and the status is left alone.
func (s *Store) RecordCallOffer(ctx context.Context, c Call) error {
	c.Status = CallRinging
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "call_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"video":    gorm.Expr("calls.video OR excluded.video"),
			"is_group": gorm.Expr("calls.is_group OR excluded.is_group"),
		}),
	}).Create(&c).Error
}

// UpdateCall applies an event to a known call. Statuses only move forward:
// answered and rejected follow ringing; termination makes a ringing call
// missed and an answered one ended. Events for unknown calls or that would
// move backwards are ignored; the result reports whether anything changed.
func (s *Store) UpdateCall(ctx context.Context, callID string, ev CallEvent, at time.Time, reason string) (bool, error) {
	var changed bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		move := func(from, to string, end bool) (bool, error) {
			set := map[string]any{"status": to}
			if end {
				set["ended_at"] = at.Unix()
				set["reason"] = reason
			}
			res := tx.Model(&Call{}).Where("call_id = ? AND status = ?", callID, from).Updates(set)
			return res.RowsAffected > 0, res.Error
		}
		var err error
		switch ev {
		case CallEventAnswered:
			changed, err = move(CallRinging, CallAnswered, false)
		case CallEventRejected:
			changed, err = move(CallRinging, CallRejected, true)
		case CallEventTerminated:
			if changed, err = move(CallRinging, CallMissed, true); err == nil && !changed {
				changed, err = move(CallAnswered, CallEnded, true)
			}
		}
		return err
	})
	return changed, err
}

// CallByID returns one call, or ErrNotFound.
func (s *Store) CallByID(ctx context.Context, callID string) (Call, error) {
	var c Call
	res := s.db.WithContext(ctx).Where("call_id = ?", callID).Limit(1).Find(&c)
	if res.Error != nil {
		return Call{}, res.Error
	}
	if res.RowsAffected == 0 {
		return Call{}, ErrNotFound
	}
	return c, nil
}

// ListCalls returns calls started at or after since, newest first.
func (s *Store) ListCalls(ctx context.Context, since time.Time, limit int) ([]Call, error) {
	tx := s.db.WithContext(ctx)
	if !since.IsZero() {
		tx = tx.Where("started_at >= ?", since.Unix())
	}
	var calls []Call
	err := tx.Order("started_at DESC").Order("call_id").Limit(clampLimit(limit)).Find(&calls).Error
	return calls, err
}
