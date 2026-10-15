// SPDX-License-Identifier: MPL-2.0

package store

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// postgresSearch indexes messages.text through a generated tsvector column.
// The 'simple' configuration is used because chats mix languages; it does no
// stemming, only lowercasing and splitting.
type postgresSearch struct{}

var postgresSearchSetup = []string{
	`ALTER TABLE messages ADD COLUMN IF NOT EXISTS search tsvector
		GENERATED ALWAYS AS (to_tsvector('simple', text)) STORED`,
	`CREATE INDEX IF NOT EXISTS idx_messages_search ON messages USING GIN (search)`,
}

func (postgresSearch) setup(tx *gorm.DB) error {
	for _, stmt := range postgresSearchSetup {
		if err := tx.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func (postgresSearch) apply(tx *gorm.DB, query string) *gorm.DB {
	return tx.
		Where("messages.search @@ websearch_to_tsquery('simple', ?)", query).
		Order(clause.OrderBy{Expression: clause.Expr{
			SQL:  "ts_rank(messages.search, websearch_to_tsquery('simple', ?)) DESC",
			Vars: []any{query},
		}})
}
