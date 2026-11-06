// SPDX-License-Identifier: MPL-2.0

package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PollVote is one person's current answer to a poll.
type PollVote struct {
	ChatJID string `gorm:"column:chat_jid;primaryKey"`
	// PollID is the poll message's ID.
	PollID string `gorm:"primaryKey"`
	// VoterJID is canonical (see Canonical).
	VoterJID string `gorm:"column:voter_jid;primaryKey"`
	// Selected is the chosen options' SHA-256 hashes, hex and comma-joined,
	// as WhatsApp sends them; empty when the vote was withdrawn. Hashes are
	// kept rather than names so a vote that arrives before its poll counts.
	Selected string `gorm:"not null;default:''"`
	// VotedAt is when the voter cast this answer, in Unix seconds.
	VotedAt int64 `gorm:"not null;default:0"`
}

// RecordPollVote stores a vote, replacing the voter's earlier one only when
// this one is at least as new, so votes may arrive in any order.
func (s *Store) RecordPollVote(ctx context.Context, v PollVote) error {
	db := s.db.WithContext(ctx)
	// A vote on a deleted poll is dropped; one that arrives before its poll counts.
	var gone int64
	if err := db.Model(&Message{}).Where("chat_jid = ? AND message_id = ? AND deleted = ?", v.ChatJID, v.PollID, true).
		Count(&gone).Error; err != nil || gone > 0 {
		return err
	}
	return upsertPollVote(db, v)
}

func upsertPollVote(tx *gorm.DB, v PollVote) error {
	newer := "CASE WHEN excluded.voted_at >= poll_votes.voted_at THEN excluded.%s ELSE poll_votes.%s END"
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "chat_jid"}, {Name: "poll_id"}, {Name: "voter_jid"}},
		DoUpdates: clause.Assignments(map[string]any{
			"selected": gorm.Expr(fmt.Sprintf(newer, "selected", "selected")),
			"voted_at": gorm.Expr(fmt.Sprintf(newer, "voted_at", "voted_at")),
		}),
	}).Create(&v).Error
}

// PollVotes returns every current vote on a poll.
func (s *Store) PollVotes(ctx context.Context, chatJID, pollID string) ([]PollVote, error) {
	var out []PollVote
	err := s.db.WithContext(ctx).Where("chat_jid = ? AND poll_id = ?", chatJID, pollID).
		Order("voted_at").Order("voter_jid").Find(&out).Error
	return out, err
}

// mergePollVotes renames oldJID to newJID as a poll's chat and as a voter.
// Each row is moved through the same upsert as a new vote, so when both
// JIDs voted on one poll the newer answer is kept.
func mergePollVotes(tx *gorm.DB, oldJID, newJID string) (int64, error) {
	var rows []PollVote
	if err := tx.Where("chat_jid = ? OR voter_jid = ?", oldJID, oldJID).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := tx.Where("chat_jid = ? OR voter_jid = ?", oldJID, oldJID).Delete(&PollVote{})
	if res.Error != nil {
		return 0, res.Error
	}
	for _, v := range rows {
		if v.ChatJID == oldJID {
			v.ChatJID = newJID
		}
		if v.VoterJID == oldJID {
			v.VoterJID = newJID
		}
		if err := upsertPollVote(tx, v); err != nil {
			return 0, err
		}
	}
	return res.RowsAffected, nil
}
