// SPDX-License-Identifier: MPL-2.0

// Package store archives chats and messages so the MCP tools can list and
// search them. WhatsApp offers no history API, so this archive is the only
// source of past messages.
package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/StephanMeijer/whatsapp-mcp/internal/db"
)

const (
	DefaultLimit = 50
	MaxLimit     = 500
	insertBatch  = 500
)

// Store reads and writes the archive.
type Store struct {
	db     *gorm.DB
	search searcher
}

// searcher is the one dialect-specific part of the store: GORM has no
// abstraction over SQLite FTS5 and Postgres full-text search.
type searcher interface {
	// setup creates the search index. It runs after AutoMigrate and must be idempotent.
	setup(tx *gorm.DB) error
	// apply restricts tx to messages matching query, ordered by relevance.
	apply(tx *gorm.DB, query string) *gorm.DB
}

// New migrates the schema and returns a Store.
func New(ctx context.Context, d *db.DB) (*Store, error) {
	var s searcher
	switch d.Dialect {
	case db.SQLite:
		s = sqliteSearch{}
	case db.Postgres:
		s = postgresSearch{}
	default:
		return nil, fmt.Errorf("store: unsupported dialect %q", d.Dialect)
	}

	g := d.Gorm.WithContext(ctx)
	if err := g.AutoMigrate(&Chat{}, &Message{}); err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	if err := s.setup(g); err != nil {
		return nil, fmt.Errorf("store: set up search: %w", err)
	}
	return &Store{db: d.Gorm, search: s}, nil
}

// Save records chats and messages. Messages already stored are left alone, so
// replaying a history sync is harmless. A chat's name is only overwritten by a
// non-empty name, and its LastMessageAt only moves forward.
func (s *Store) Save(ctx context.Context, chats []Chat, msgs []Message) error {
	byJID := make(map[string]*Chat, len(chats))
	order := make([]string, 0, len(chats))
	upsert := func(c Chat) {
		existing, ok := byJID[c.JID]
		if !ok {
			byJID[c.JID] = &c
			order = append(order, c.JID)
			return
		}
		if c.Name != "" {
			existing.Name = c.Name
		}
		existing.LastMessageAt = max(existing.LastMessageAt, c.LastMessageAt)
	}
	for _, c := range chats {
		upsert(c)
	}
	for _, m := range msgs {
		upsert(Chat{JID: m.ChatJID, LastMessageAt: m.Timestamp})
	}
	merged := make([]Chat, 0, len(order))
	for _, jid := range order {
		merged = append(merged, *byJID[jid])
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(merged) > 0 {
			err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "jid"}},
				DoUpdates: clause.Assignments(map[string]any{
					"name": gorm.Expr("CASE WHEN excluded.name <> '' THEN excluded.name ELSE chats.name END"),
					"last_message_at": gorm.Expr(
						"CASE WHEN excluded.last_message_at > chats.last_message_at " +
							"THEN excluded.last_message_at ELSE chats.last_message_at END"),
				}),
			}).CreateInBatches(merged, insertBatch).Error
			if err != nil {
				return fmt.Errorf("save chats: %w", err)
			}
		}
		if len(msgs) > 0 {
			rows := make([]Message, len(msgs))
			copy(rows, msgs)
			for i := range rows {
				rows[i].Seq = 0
			}
			err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "chat_jid"}, {Name: "message_id"}},
				DoNothing: true,
			}).CreateInBatches(rows, insertBatch).Error
			if err != nil {
				return fmt.Errorf("save messages: %w", err)
			}
		}
		return nil
	})
}

// RenameChats sets the names of chats that already exist, keyed by JID. Unknown
// JIDs are ignored, so a whole address book can be passed without creating a
// chat per contact. It returns the number of chats whose name changed.
func (s *Store) RenameChats(ctx context.Context, names map[string]string) (int64, error) {
	var changed int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for jid, name := range names {
			if name == "" {
				continue
			}
			res := tx.Model(&Chat{}).Where("jid = ? AND name <> ?", jid, name).Update("name", name)
			if res.Error != nil {
				return fmt.Errorf("rename chat %s: %w", jid, res.Error)
			}
			changed += res.RowsAffected
		}
		return nil
	})
	return changed, err
}

// ListChats returns chats ordered by most recent activity. A non-empty query
// filters on name or JID, case-insensitively.
func (s *Store) ListChats(ctx context.Context, query string, limit int) ([]Chat, error) {
	tx := s.db.WithContext(ctx).Model(&Chat{})
	if query != "" {
		pattern := "%" + escapeLike(strings.ToLower(query)) + "%"
		tx = tx.Where(`LOWER(name) LIKE ? ESCAPE '\' OR LOWER(jid) LIKE ? ESCAPE '\'`, pattern, pattern)
	}
	var chats []Chat
	err := tx.Order("last_message_at DESC").Order("jid").Limit(clampLimit(limit)).Find(&chats).Error
	return chats, err
}

// ListMessages returns a chat's messages, newest first. A non-zero before
// returns only messages sent strictly before that time, for paging.
func (s *Store) ListMessages(ctx context.Context, chatJID string, before time.Time, limit int) ([]Message, error) {
	tx := s.db.WithContext(ctx).Where("chat_jid = ?", chatJID)
	if !before.IsZero() {
		tx = tx.Where("timestamp < ?", before.Unix())
	}
	var msgs []Message
	err := tx.Order("timestamp DESC").Order("seq DESC").Limit(clampLimit(limit)).Find(&msgs).Error
	return msgs, err
}

// SearchMessages full-text searches message text, optionally within one chat,
// best matches first.
func (s *Store) SearchMessages(ctx context.Context, query, chatJID string, limit int) ([]Message, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("search query is empty")
	}
	tx := s.search.apply(s.db.WithContext(ctx).Model(&Message{}), query)
	if chatJID != "" {
		tx = tx.Where("messages.chat_jid = ?", chatJID)
	}
	var msgs []Message
	err := tx.Order("messages.timestamp DESC").Limit(clampLimit(limit)).Find(&msgs).Error
	return msgs, err
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	return min(limit, MaxLimit)
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
