// SPDX-License-Identifier: MPL-2.0

// Package config loads runtime configuration from flags and the environment.
package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the runtime configuration of the server.
type Config struct {
	// Addr is the address the HTTP server listens on.
	Addr string
	// DatabaseURL selects the database, e.g. sqlite://data/whatsapp.db or postgres://….
	DatabaseURL string
	// AuthToken is the bearer token every MCP request must carry.
	AuthToken string
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
	// ReadOnly leaves out every tool that changes anything on WhatsApp.
	ReadOnly bool
	// AllowRecipients, when non-empty, limits write tools to these JIDs or
	// phone numbers (plus the account's own number).
	AllowRecipients []string
	// MediaDir, when set, is the only folder send_media may read files from.
	MediaDir string
}

// Load parses args (without the program name). Flags take precedence over
// environment variables, which take precedence over defaults. The auth token
// is read from the environment only, so it never shows up in a process list.
func Load(args []string, getenv func(string) string) (Config, error) {
	envOr := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}

	readOnlyDefault := false
	if v := getenv("WHATSAPP_MCP_READ_ONLY"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("WHATSAPP_MCP_READ_ONLY: %w", err)
		}
		readOnlyDefault = b
	}

	cfg := Config{AuthToken: getenv("WHATSAPP_MCP_TOKEN")}
	var allow string
	fs := flag.NewFlagSet("whatsapp-mcp", flag.ContinueOnError)
	fs.BoolVar(&cfg.ReadOnly, "read-only", readOnlyDefault, "hide all tools that send or change anything (env WHATSAPP_MCP_READ_ONLY)")
	fs.StringVar(&cfg.MediaDir, "media-dir", getenv("WHATSAPP_MCP_MEDIA_DIR"), "folder send_media may read files from; empty disables file paths (env WHATSAPP_MCP_MEDIA_DIR)")
	fs.StringVar(&allow, "allow-recipients", getenv("WHATSAPP_MCP_ALLOW_RECIPIENTS"), "comma-separated JIDs or phone numbers write tools may target; empty allows all (env WHATSAPP_MCP_ALLOW_RECIPIENTS)")
	fs.StringVar(&cfg.Addr, "addr", envOr("WHATSAPP_MCP_ADDR", "127.0.0.1:8080"), "HTTP listen address (env WHATSAPP_MCP_ADDR)")
	fs.StringVar(&cfg.DatabaseURL, "database-url", envOr("DATABASE_URL", "sqlite://data/whatsapp.db"), "sqlite://path or postgres://… (env DATABASE_URL)")
	fs.StringVar(&cfg.LogLevel, "log-level", envOr("WHATSAPP_MCP_LOG_LEVEL", "info"), "debug, info, warn or error (env WHATSAPP_MCP_LOG_LEVEL)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if cfg.AuthToken == "" {
		return Config{}, errors.New("WHATSAPP_MCP_TOKEN must be set: the server exposes your WhatsApp account")
	}
	cfg.AllowRecipients = splitList(allow)
	if cfg.MediaDir != "" {
		info, err := os.Stat(cfg.MediaDir)
		if err != nil {
			return Config{}, fmt.Errorf("-media-dir: %w", err)
		}
		if !info.IsDir() {
			return Config{}, fmt.Errorf("-media-dir: %s is not a directory", cfg.MediaDir)
		}
	}
	if cfg.ReadOnly && len(cfg.AllowRecipients) > 0 {
		return Config{}, errors.New("-allow-recipients has no effect with -read-only; set one or the other")
	}
	return cfg, nil
}

// splitList splits a comma-separated list, trimming spaces and dropping empty entries.
func splitList(s string) []string {
	var out []string
	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
