// SPDX-License-Identifier: MPL-2.0

package db

import (
	"os"
	"path/filepath"
	"strings"
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

func TestOpenErrors(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, url, want string
	}{
		{"unparsable URL", "sqlite://%zz", "parse database URL"},
		{"sqlite directory cannot be created", "sqlite://" + filepath.Join(notADir, "wa.db"), "create database directory"},
		// Port 1 on loopback refuses at once, so this fails fast without a server.
		{"postgres unreachable", "postgres://u:p@127.0.0.1:1/wa?sslmode=disable&connect_timeout=5", "open postgres database"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := Open(tt.url)
			if err == nil {
				d.Close()
				t.Fatalf("Open(%q): want error", tt.url)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Open(%q) = %v; want error containing %q", tt.url, err, tt.want)
			}
		})
	}
}

func TestOpenUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes to any directory")
	}
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	d, err := Open("sqlite://" + filepath.Join(dir, "wa.db"))
	if err == nil {
		d.Close()
		t.Fatal("Open in a read-only directory: want error")
	}
	if !strings.Contains(err.Error(), "not writable") || !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not say that %s is not writable", err, dir)
	}
}
