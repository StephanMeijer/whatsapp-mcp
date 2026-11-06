// SPDX-License-Identifier: MPL-2.0

package store

import (
	"context"

	"gorm.io/gorm/clause"
)

// ChatFlags is a change to a chat's archived, pinned, muted or disappearing
// state; a nil field is left as it is.
type ChatFlags struct {
	Archived            *bool
	Pinned              *bool
	MutedUntil          *int64 // Unix seconds; 0 unmutes, -1 mutes forever
	DisappearingSeconds *int64 // disappearing-message timer; 0 turns it off
}

// ArchiveFilter says which chats ListChats returns.
type ArchiveFilter int

const (
	ExcludeArchived ArchiveFilter = iota
	IncludeArchived
	OnlyArchived
)

// SetChatFlags applies f to a chat, creating the chat if the archive has
// never seen it (a chat archived long ago may have no messages here).
func (s *Store) SetChatFlags(ctx context.Context, jid string, f ChatFlags) error {
	c := Chat{JID: jid}
	var cols []string
	if f.Archived != nil {
		c.Archived, cols = *f.Archived, append(cols, "archived")
	}
	if f.Pinned != nil {
		c.Pinned, cols = *f.Pinned, append(cols, "pinned")
	}
	if f.MutedUntil != nil {
		c.MutedUntil, cols = *f.MutedUntil, append(cols, "muted_until")
	}
	if f.DisappearingSeconds != nil {
		c.DisappearingSeconds, cols = *f.DisappearingSeconds, append(cols, "disappearing_seconds")
	}
	if len(cols) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "jid"}},
		DoUpdates: clause.AssignmentColumns(cols),
	}).Create(&c).Error
}
