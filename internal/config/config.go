// SPDX-License-Identifier: MPL-2.0

// Package config loads runtime configuration from flags and the environment.
package config

import (
	"errors"
	"flag"
	"fmt"
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

	cfg := Config{AuthToken: getenv("WHATSAPP_MCP_TOKEN")}
	fs := flag.NewFlagSet("whatsapp-mcp", flag.ContinueOnError)
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
	return cfg, nil
}
