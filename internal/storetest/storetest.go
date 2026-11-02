// SPDX-License-Identifier: MPL-2.0

// Package storetest provides a throwaway SQLite-backed store and database for tests.
package storetest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/db"
	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// NewDB returns a fresh SQLite database that is closed when t ends.
func NewDB(t testing.TB) *db.DB {
	t.Helper()
	d, err := db.Open("sqlite://" + filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// New returns a store on a fresh SQLite database that is closed when t ends.
func New(t testing.TB) *store.Store {
	t.Helper()
	s, err := store.New(context.Background(), NewDB(t))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
