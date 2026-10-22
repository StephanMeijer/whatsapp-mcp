// SPDX-License-Identifier: MPL-2.0

// Package store archives chats and messages so the MCP tools can list and
// search them. WhatsApp offers no history API, so this archive is the only
// source of past messages.
package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	if err := g.AutoMigrate(&Chat{}, &Message{}, &Call{}, &Receipt{}); err != nil {
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
			// An already archived message is left as it is, except that a
			// replay may supply the media reference it was archived without.
			// An edited text message is not given one: the replay holds its
			// pre-edit text.
			err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "chat_jid"}, {Name: "message_id"}},
				DoUpdates: clause.Assignments(map[string]any{
					// An absent ref may be stored as NULL or as an empty blob.
					"media_ref": gorm.Expr("CASE WHEN messages.deleted THEN messages.media_ref " +
						"WHEN messages.media_type = '' AND messages.edited_at > 0 THEN messages.media_ref " +
						"WHEN messages.media_ref IS NULL OR length(messages.media_ref) = 0 THEN excluded.media_ref " +
						"ELSE messages.media_ref END"),
				}),
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

// LIDJIDs returns the distinct @lid JIDs used as chat or sender, the
// candidates for MergeJIDs once their phone number is known.
func (s *Store) LIDJIDs(ctx context.Context) ([]string, error) {
	var jids []string
	err := s.db.WithContext(ctx).Raw(
		`SELECT jid FROM chats WHERE jid LIKE '%@lid'
		 UNION
		 SELECT DISTINCT sender_jid FROM messages WHERE sender_jid LIKE '%@lid'`,
	).Scan(&jids).Error
	return jids, err
}

// MergeJIDs moves everything archived under each old JID to its new JID:
// senders and receipt participants are rewritten, messages and their
// receipts move to the new chat (dropping copies the new chat already has),
// and the chat rows are merged, keeping the new chat's
// name when it has one and the later LastMessageAt. It returns how many old
// JIDs had anything to move, so a second run returns 0.
func (s *Store) MergeJIDs(ctx context.Context, renames map[string]string) (int, error) {
	merged := 0
	for oldJID, newJID := range renames {
		if oldJID == newJID || newJID == "" {
			continue
		}
		var changed bool
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var err error
			changed, err = mergeJID(tx, oldJID, newJID)
			return err
		})
		if err != nil {
			return merged, fmt.Errorf("merge %s into %s: %w", oldJID, newJID, err)
		}
		if changed {
			merged++
		}
	}
	return merged, nil
}

func mergeJID(tx *gorm.DB, oldJID, newJID string) (bool, error) {
	var affected int64
	res := tx.Model(&Message{}).Where("sender_jid = ?", oldJID).Update("sender_jid", newJID)
	if res.Error != nil {
		return false, res.Error
	}
	affected += res.RowsAffected

	// Receipts follow their messages, so they are handled around the message
	// moves: those of dropped duplicates go first, the rest move with the chat.
	copies := func() *gorm.DB {
		return tx.Model(&Message{}).Select("message_id").Where("chat_jid = ?", newJID)
	}
	res = tx.Where("chat_jid = ? AND message_id IN (?)", oldJID, copies()).Delete(&Receipt{})
	if res.Error != nil {
		return false, res.Error
	}
	affected += res.RowsAffected

	res = tx.Where("chat_jid = ? AND message_id IN (?)", oldJID, copies()).Delete(&Message{})
	if res.Error != nil {
		return false, res.Error
	}
	affected += res.RowsAffected

	res = tx.Model(&Message{}).Where("chat_jid = ?", oldJID).Update("chat_jid", newJID)
	if res.Error != nil {
		return false, res.Error
	}
	affected += res.RowsAffected

	res = tx.Model(&Receipt{}).Where("chat_jid = ?", oldJID).Update("chat_jid", newJID)
	if res.Error != nil {
		return false, res.Error
	}
	affected += res.RowsAffected

	n, err := mergeParticipant(tx, oldJID, newJID)
	if err != nil {
		return false, err
	}
	affected += n

	var old Chat
	res = tx.Where("jid = ?", oldJID).Limit(1).Find(&old)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected > 0 {
		var target Chat
		if err := tx.Where("jid = ?", newJID).Limit(1).Find(&target).Error; err != nil {
			return false, err
		}
		target.JID = newJID
		if target.Name == "" {
			target.Name = old.Name
		}
		target.LastMessageAt = max(target.LastMessageAt, old.LastMessageAt)
		target.Archived = target.Archived || old.Archived
		target.Pinned = target.Pinned || old.Pinned
		target.MutedUntil = max(target.MutedUntil, old.MutedUntil)
		if err := tx.Save(&target).Error; err != nil {
			return false, err
		}
		if err := tx.Delete(&Chat{}, "jid = ?", oldJID).Error; err != nil {
			return false, err
		}
		affected++
	}
	return affected > 0, nil
}

// mergeParticipant renames oldJID to newJID as a receipt participant. When the
// message already has a receipt from newJID, the two are combined, keeping
// the later time in each column. It returns the number of rows renamed.
func mergeParticipant(tx *gorm.DB, oldJID, newJID string) (int64, error) {
	var rows []Receipt
	if err := tx.Where("participant_jid = ?", oldJID).Find(&rows).Error; err != nil {
		return 0, err
	}
	later := func(column string) string {
		return fmt.Sprintf("CASE WHEN excluded.%[1]s > receipts.%[1]s THEN excluded.%[1]s ELSE receipts.%[1]s END", column)
	}
	for _, r := range rows {
		moved := r
		moved.ParticipantJID = newJID
		err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "chat_jid"}, {Name: "message_id"}, {Name: "participant_jid"}},
			DoUpdates: clause.Assignments(map[string]any{
				"delivered_at": gorm.Expr(later("delivered_at")),
				"read_at":      gorm.Expr(later("read_at")),
				"played_at":    gorm.Expr(later("played_at")),
			}),
		}).Create(&moved).Error
		if err != nil {
			return 0, err
		}
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := tx.Where("participant_jid = ?", oldJID).Delete(&Receipt{})
	return res.RowsAffected, res.Error
}

// ListChats returns chats, pinned first, then by most recent activity. A non-empty query
// filters on name or JID, case-insensitively.
func (s *Store) ListChats(ctx context.Context, query string, archived ArchiveFilter, limit int) ([]Chat, error) {
	tx := s.db.WithContext(ctx).Model(&Chat{})
	if query != "" {
		pattern := "%" + escapeLike(strings.ToLower(query)) + "%"
		tx = tx.Where(`LOWER(name) LIKE ? ESCAPE '\' OR LOWER(jid) LIKE ? ESCAPE '\'`, pattern, pattern)
	}
	switch archived {
	case ExcludeArchived:
		tx = tx.Where("archived = ?", false)
	case OnlyArchived:
		tx = tx.Where("archived = ?", true)
	}
	var chats []Chat
	err := tx.Order("pinned DESC").Order("last_message_at DESC").Order("jid").Limit(clampLimit(limit)).Find(&chats).Error
	return chats, err
}

// ListMessages returns a chat's messages, newest first. A non-zero before
// returns only messages sent strictly before that time, for paging.
func (s *Store) ListMessages(ctx context.Context, chatJID string, before time.Time, limit int) ([]Message, error) {
	tx := s.db.WithContext(ctx).Where("chat_jid = ? AND deleted = ?", chatJID, false)
	if !before.IsZero() {
		tx = tx.Where("timestamp < ?", before.Unix())
	}
	var msgs []Message
	err := tx.Order("timestamp DESC").Order("seq DESC").Limit(clampLimit(limit)).Find(&msgs).Error
	return msgs, err
}

// RecentQuery selects messages in a time window, for RecentMessages.
type RecentQuery struct {
	// Since is inclusive and required; Until is exclusive and optional.
	Since, Until time.Time
	// ChatJID restricts the result to one chat when set.
	ChatJID string
	// IncludeFromMe also returns the account's own messages.
	IncludeFromMe bool
	Limit         int
}

// RecentMessages returns messages in the window, newest first. When more
// than the limit match, it returns the newest ones and reports truncated.
func (s *Store) RecentMessages(ctx context.Context, q RecentQuery) (msgs []Message, truncated bool, err error) {
	limit := clampLimit(q.Limit)
	tx := s.db.WithContext(ctx).Where("timestamp >= ? AND deleted = ?", q.Since.Unix(), false)
	if !q.Until.IsZero() {
		tx = tx.Where("timestamp < ?", q.Until.Unix())
	}
	if q.ChatJID != "" {
		tx = tx.Where("chat_jid = ?", q.ChatJID)
	}
	if !q.IncludeFromMe {
		tx = tx.Where("from_me = ?", false)
	}
	err = tx.Order("timestamp DESC").Order("seq DESC").Limit(limit + 1).Find(&msgs).Error
	if err != nil {
		return nil, false, err
	}
	if len(msgs) > limit {
		return msgs[:limit], true, nil
	}
	return msgs, false, nil
}

// ApplyEdit replaces an archived message's content with an edit made at
// editedAt (Unix seconds). It reports false when nothing changed: the message
// isn't archived, was deleted, or already reflects a later edit, so late or
// replayed edits never overwrite newer content.
//
// ref is the new media_ref of a text message, whose ref (if any) holds the
// pre-edit text and so is always replaced: pass nil unless the edit has to be
// rebuilt later. A message with media keeps its ref, which downloads need.
func (s *Store) ApplyEdit(ctx context.Context, chatJID, messageID, text, mediaType string, ref []byte, editedAt int64) (bool, error) {
	res := s.db.WithContext(ctx).Model(&Message{}).
		Where("chat_jid = ? AND message_id = ? AND deleted = ? AND edited_at < ?", chatJID, messageID, false, editedAt).
		Updates(map[string]any{
			"text":       text,
			"media_type": mediaType,
			"media_ref":  gorm.Expr("CASE WHEN messages.media_type = '' THEN ? ELSE messages.media_ref END", ref),
			"edited_at":  editedAt,
		})
	return res.RowsAffected > 0, res.Error
}

// MarkDeleted records that a message was deleted for everyone and clears its
// text, removing it from the search index too. The row is kept so a later
// edit or replay cannot bring the message back. It reports false when the
// message isn't archived or was already deleted.
func (s *Store) MarkDeleted(ctx context.Context, chatJID, messageID string) (bool, error) {
	res := s.db.WithContext(ctx).Model(&Message{}).
		Where("chat_jid = ? AND message_id = ? AND deleted = ?", chatJID, messageID, false).
		Updates(map[string]any{"deleted": true, "text": "", "media_ref": nil})
	return res.RowsAffected > 0, res.Error
}

// ErrNotFound is returned when a requested message is not in the archive.
var ErrNotFound = errors.New("not found")

// MessageContext returns a message with up to before messages preceding it
// and up to after messages following it in the same chat, oldest first, and
// the index of the message itself in that slice. Order matches ListMessages:
// by timestamp, then archive sequence for messages in the same second.
func (s *Store) MessageContext(ctx context.Context, chatJID, messageID string, before, after int) ([]Message, int, error) {
	db := s.db.WithContext(ctx)
	var target Message
	res := db.Where("chat_jid = ? AND message_id = ? AND deleted = ?", chatJID, messageID, false).Limit(1).Find(&target)
	if res.Error != nil {
		return nil, 0, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, 0, ErrNotFound
	}

	var prev, next []Message
	if before > 0 {
		err := db.Where("chat_jid = ? AND deleted = ? AND (timestamp < ? OR (timestamp = ? AND seq < ?))",
			chatJID, false, target.Timestamp, target.Timestamp, target.Seq).
			Order("timestamp DESC").Order("seq DESC").Limit(before).Find(&prev).Error
		if err != nil {
			return nil, 0, err
		}
		slices.Reverse(prev)
	}
	if after > 0 {
		err := db.Where("chat_jid = ? AND deleted = ? AND (timestamp > ? OR (timestamp = ? AND seq > ?))",
			chatJID, false, target.Timestamp, target.Timestamp, target.Seq).
			Order("timestamp ASC").Order("seq ASC").Limit(after).Find(&next).Error
		if err != nil {
			return nil, 0, err
		}
	}

	out := make([]Message, 0, len(prev)+1+len(next))
	out = append(out, prev...)
	out = append(out, target)
	out = append(out, next...)
	return out, len(prev), nil
}

// MessageByID returns one archived message. Deleted messages count as not
// found, since nothing should be done with them.
func (s *Store) MessageByID(ctx context.Context, chatJID, messageID string) (Message, error) {
	var m Message
	res := s.db.WithContext(ctx).
		Where("chat_jid = ? AND message_id = ? AND deleted = ?", chatJID, messageID, false).Limit(1).Find(&m)
	if res.Error != nil {
		return Message{}, res.Error
	}
	if res.RowsAffected == 0 {
		return Message{}, ErrNotFound
	}
	return m, nil
}

// LatestIncoming returns up to limit of the newest messages others sent in a
// chat, newest first.
func (s *Store) LatestIncoming(ctx context.Context, chatJID string, limit int) ([]Message, error) {
	var msgs []Message
	err := s.db.WithContext(ctx).
		Where("chat_jid = ? AND from_me = ? AND deleted = ?", chatJID, false, false).
		Order("timestamp DESC").Order("seq DESC").Limit(clampLimit(limit)).Find(&msgs).Error
	return msgs, err
}

// OldestMessage returns the earliest archived message in a chat, the anchor
// for asking the phone for older history. It returns ErrNotFound for a chat
// without messages.
func (s *Store) OldestMessage(ctx context.Context, chatJID string) (Message, error) {
	var m Message
	res := s.db.WithContext(ctx).Where("chat_jid = ?", chatJID).
		Order("timestamp ASC").Order("seq ASC").Limit(1).Find(&m)
	if res.Error != nil {
		return Message{}, res.Error
	}
	if res.RowsAffected == 0 {
		return Message{}, ErrNotFound
	}
	return m, nil
}

// CountBefore counts a chat's messages sent strictly before ts (Unix seconds).
func (s *Store) CountBefore(ctx context.Context, chatJID string, ts int64) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Message{}).
		Where("chat_jid = ? AND timestamp < ?", chatJID, ts).Count(&n).Error
	return n, err
}

// ChatsByJID returns the stored chats among jids, keyed by JID.
func (s *Store) ChatsByJID(ctx context.Context, jids []string) (map[string]Chat, error) {
	out := make(map[string]Chat, len(jids))
	if len(jids) == 0 {
		return out, nil
	}
	var chats []Chat
	if err := s.db.WithContext(ctx).Where("jid IN ?", jids).Find(&chats).Error; err != nil {
		return nil, err
	}
	for _, c := range chats {
		out[c.JID] = c
	}
	return out, nil
}

// SearchMessages full-text searches message text, optionally within one chat,
// best matches first.
func (s *Store) SearchMessages(ctx context.Context, query, chatJID string, limit int) ([]Message, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("search query is empty")
	}
	tx := s.search.apply(s.db.WithContext(ctx).Model(&Message{}), query).Where("messages.deleted = ?", false)
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
