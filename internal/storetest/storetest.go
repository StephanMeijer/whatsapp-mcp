// SPDX-License-Identifier: MPL-2.0

// Package storetest provides a throwaway SQLite-backed store for tests.
package storetest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/db"
	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// New returns a store on a fresh SQLite database that is closed when t ends.
func New(t testing.TB) *store.Store {
	t.Helper()
	d, err := db.Open("sqlite://" + filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	s, err := store.New(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
