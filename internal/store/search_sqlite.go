// SPDX-License-Identifier: MPL-2.0

package store

import (
	"strings"

	"gorm.io/gorm"
)

// sqliteSearch indexes messages.text in an external-content FTS5 table kept
// in sync by triggers.
type sqliteSearch struct{}

var sqliteSearchSetup = []string{
	`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
		text, content='messages', content_rowid='seq',
		tokenize='unicode61 remove_diacritics 2')`,
	`CREATE TRIGGER IF NOT EXISTS messages_fts_ai AFTER INSERT ON messages BEGIN
		INSERT INTO messages_fts(rowid, text) VALUES (new.seq, new.text);
	END`,
	`CREATE TRIGGER IF NOT EXISTS messages_fts_ad AFTER DELETE ON messages BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.seq, old.text);
	END`,
	`CREATE TRIGGER IF NOT EXISTS messages_fts_au AFTER UPDATE OF text ON messages BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.seq, old.text);
		INSERT INTO messages_fts(rowid, text) VALUES (new.seq, new.text);
	END`,
}

func (sqliteSearch) setup(tx *gorm.DB) error {
	var existing int64
	if err := tx.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'messages_fts'`).Scan(&existing).Error; err != nil {
		return err
	}
	for _, stmt := range sqliteSearchSetup {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	// A new index over a table that already has rows must be filled once;
	// the triggers only cover changes made after they exist.
	if existing == 0 {
		return tx.Exec(`INSERT INTO messages_fts(messages_fts) VALUES ('rebuild')`).Error
	}
	return nil
}

func (sqliteSearch) apply(tx *gorm.DB, query string) *gorm.DB {
	return tx.
		Joins("JOIN messages_fts ON messages_fts.rowid = messages.seq").
		Where("messages_fts MATCH ?", ftsQuery(query)).
		Order("messages_fts.rank")
}

// ftsQuery turns free text into an FTS5 query that matches all words, each as
// a literal phrase, so user input can never be a syntax error.
func ftsQuery(query string) string {
	words := strings.Fields(query)
	for i, w := range words {
		words[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}
	return strings.Join(words, " ")
}
