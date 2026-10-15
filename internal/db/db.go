// SPDX-License-Identifier: MPL-2.0

// Package db opens the single database shared by the message archive (via
// GORM) and whatsmeow's device store (via the underlying *sql.DB).
package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Dialect names, as understood by whatsmeow's sqlstore.
const (
	SQLite   = "sqlite3"
	Postgres = "postgres"
)

// DB is an open database connection.
type DB struct {
	Gorm    *gorm.DB
	SQL     *sql.DB
	Dialect string
}

// Open connects to the database named by rawURL, which is either
// sqlite://<path> or a postgres:// / postgresql:// URL.
func Open(rawURL string) (*DB, error) {
	dialect, dsn, err := parse(rawURL)
	if err != nil {
		return nil, err
	}

	var dialector gorm.Dialector
	switch dialect {
	case SQLite:
		if dir := filepath.Dir(dsn); dir != "." {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, fmt.Errorf("create database directory: %w", err)
			}
		}
		dialector = sqlite.Open(sqliteDSN(dsn))
	case Postgres:
		dialector = postgres.Open(dsn)
	}

	g, err := gorm.Open(dialector, &gorm.Config{
		Logger:      logger.Default.LogMode(logger.Silent),
		QueryFields: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", dialect, err)
	}
	sqlDB, err := g.DB()
	if err != nil {
		return nil, err
	}
	return &DB{Gorm: g, SQL: sqlDB, Dialect: dialect}, nil
}

// Close closes the underlying connection pool.
func (d *DB) Close() error { return d.SQL.Close() }

func parse(rawURL string) (dialect, dsn string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("parse database URL: %w", err)
	}
	switch u.Scheme {
	case "sqlite", "sqlite3":
		path := strings.TrimPrefix(rawURL, u.Scheme+"://")
		if path == "" || path == rawURL {
			return "", "", fmt.Errorf("database URL %q: want sqlite://<path>", rawURL)
		}
		return SQLite, path, nil
	case "postgres", "postgresql":
		return Postgres, rawURL, nil
	default:
		return "", "", fmt.Errorf("database URL: unsupported scheme %q (want sqlite or postgres)", u.Scheme)
	}
}

// sqliteDSN enables what whatsmeow requires (foreign keys) and what concurrent
// use from the event loop and HTTP handlers needs (WAL, busy timeout, and
// write transactions that take the lock up front).
func sqliteDSN(path string) string {
	return "file:" + path +
		"?_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(10000)" +
		"&_txlock=immediate"
}
