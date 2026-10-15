// SPDX-License-Identifier: MPL-2.0

package db

import (
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		url, dialect, dsn string
		wantErr           bool
	}{
		{url: "sqlite://data/whatsapp.db", dialect: SQLite, dsn: "data/whatsapp.db"},
		{url: "sqlite:///var/lib/wa.db", dialect: SQLite, dsn: "/var/lib/wa.db"},
		{url: "postgres://u:p@localhost/wa", dialect: Postgres, dsn: "postgres://u:p@localhost/wa"},
		{url: "postgresql://localhost/wa?sslmode=disable", dialect: Postgres, dsn: "postgresql://localhost/wa?sslmode=disable"},
		{url: "sqlite://", wantErr: true},
		{url: "mysql://localhost/wa", wantErr: true},
		{url: "data/whatsapp.db", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			dialect, dsn, err := parse(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parse(%q): want error, got %q %q", tt.url, dialect, dsn)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse(%q): %v", tt.url, err)
			}
			if dialect != tt.dialect || dsn != tt.dsn {
				t.Errorf("parse(%q) = %q, %q; want %q, %q", tt.url, dialect, dsn, tt.dialect, tt.dsn)
			}
		})
	}
}

func TestOpenSQLiteEnablesForeignKeys(t *testing.T) {
	d, err := Open("sqlite://" + filepath.Join(t.TempDir(), "nested", "wa.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var fk int
	if err := d.SQL.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1 (whatsmeow requires it)", fk)
	}
}
