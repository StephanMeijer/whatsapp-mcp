// SPDX-License-Identifier: MPL-2.0

// Package config loads runtime configuration from flags and the environment.
package config

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/StephanMeijer/whatsapp-mcp/internal/httpx"
)

// MinTokenLength is the shortest auth token Load accepts: 32 characters, or
// 128 bits of random hex. The token is all that stands between the network
// and the account, so a guessable one is refused rather than warned about.
const MinTokenLength = 32

// DefaultAddr is the listen address when neither -addr nor
// WHATSAPP_MCP_ADDR is set.
const DefaultAddr = "127.0.0.1:8080"

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
	// ExpireDisappearing deletes archived messages from disappearing chats
	// once their timer runs out, as they vanish for the people in the chat.
	ExpireDisappearing bool
	// EnableGroupAdmin offers the tools that create, change and leave
	// groups. They are hard to undo, so they are off unless asked for.
	EnableGroupAdmin bool
	// AllowInviteLinks lets group_invite_link work while an allowlist is
	// set, though anyone with a link can join a group regardless of it.
	AllowInviteLinks bool
	// AllowStatus lets post_status work while an allowlist is set, though a
	// status goes to your whole status audience, not to listed chats.
	AllowStatus bool
	// AllowRecipients, when non-empty, limits write tools to these JIDs or
	// phone numbers (plus the account's own number).
	AllowRecipients []string
	// MediaDir, when set, is the only folder send_media may read files from.
	MediaDir string
	// PublicURL, when set, is the origin clients reach the server at through
	// a TLS reverse proxy, such as https://wa.example.com: lowercase, without
	// a trailing slash or path. Requests for any other host than this one or
	// a loopback address are refused.
	PublicURL string
}

// Load parses args (without the program name). Flags take precedence over
// environment variables, which take precedence over defaults. The auth token
// is read from the environment only, so it never shows up in a process list.
func Load(args []string, getenv func(string) string) (Config, error) {
	readOnlyDefault, err := envBool(getenv, "WHATSAPP_MCP_READ_ONLY")
	if err != nil {
		return Config{}, err
	}
	expireDefault, err := envBool(getenv, "WHATSAPP_MCP_EXPIRE_DISAPPEARING")
	if err != nil {
		return Config{}, err
	}
	groupAdminDefault, err := envBool(getenv, "WHATSAPP_MCP_ENABLE_GROUP_ADMIN")
	if err != nil {
		return Config{}, err
	}
	inviteLinksDefault, err := envBool(getenv, "WHATSAPP_MCP_ALLOW_INVITE_LINKS")
	if err != nil {
		return Config{}, err
	}
	statusDefault, err := envBool(getenv, "WHATSAPP_MCP_ALLOW_STATUS")
	if err != nil {
		return Config{}, err
	}

	cfg := Config{AuthToken: getenv("WHATSAPP_MCP_TOKEN")}
	var allow string
	fs := flag.NewFlagSet("whatsapp-mcp", flag.ContinueOnError)
	fs.BoolVar(&cfg.ReadOnly, "read-only", readOnlyDefault, "hide all tools that send or change anything (env WHATSAPP_MCP_READ_ONLY)")
	fs.BoolVar(&cfg.EnableGroupAdmin, "enable-group-admin", groupAdminDefault, "offer the tools that create, change and leave groups (env WHATSAPP_MCP_ENABLE_GROUP_ADMIN)")
	fs.BoolVar(&cfg.AllowInviteLinks, "allow-invite-links", inviteLinksDefault, "let group_invite_link work while -allow-recipients is set; a link lets anyone join (env WHATSAPP_MCP_ALLOW_INVITE_LINKS)")
	fs.BoolVar(&cfg.AllowStatus, "allow-status", statusDefault, "let post_status work while -allow-recipients is set; a status goes to your whole status audience (env WHATSAPP_MCP_ALLOW_STATUS)")
	fs.BoolVar(&cfg.ExpireDisappearing, "expire-disappearing", expireDefault, "delete archived messages from disappearing chats once their timer runs out (env WHATSAPP_MCP_EXPIRE_DISAPPEARING)")
	fs.StringVar(&cfg.MediaDir, "media-dir", getenv("WHATSAPP_MCP_MEDIA_DIR"), "folder send_media may read files from; empty disables file paths (env WHATSAPP_MCP_MEDIA_DIR)")
	fs.StringVar(&allow, "allow-recipients", getenv("WHATSAPP_MCP_ALLOW_RECIPIENTS"), "comma-separated JIDs or phone numbers write tools may target; empty allows all (env WHATSAPP_MCP_ALLOW_RECIPIENTS)")
	fs.StringVar(&cfg.PublicURL, "public-url", getenv("WHATSAPP_MCP_PUBLIC_URL"), "https origin clients reach this server at through a reverse proxy, e.g. https://wa.example.com (env WHATSAPP_MCP_PUBLIC_URL)")
	fs.StringVar(&cfg.Addr, "addr", cmp.Or(getenv("WHATSAPP_MCP_ADDR"), DefaultAddr), "HTTP listen address (env WHATSAPP_MCP_ADDR)")
	fs.StringVar(&cfg.DatabaseURL, "database-url", cmp.Or(getenv("DATABASE_URL"), "sqlite://data/whatsapp.db"), "sqlite://path or postgres://… (env DATABASE_URL)")
	fs.StringVar(&cfg.LogLevel, "log-level", cmp.Or(getenv("WHATSAPP_MCP_LOG_LEVEL"), "info"), "debug, info, warn or error (env WHATSAPP_MCP_LOG_LEVEL)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if cfg.AuthToken == "" {
		return Config{}, errors.New("WHATSAPP_MCP_TOKEN must be set: the server exposes your WhatsApp account")
	}
	if len(cfg.AuthToken) < MinTokenLength {
		return Config{}, fmt.Errorf("WHATSAPP_MCP_TOKEN must be at least %d characters, as it gives full access to "+
			"your WhatsApp account; generate one with: openssl rand -hex 32", MinTokenLength)
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
	if cfg.PublicURL != "" {
		u, err := publicURL(cfg.PublicURL)
		if err != nil {
			return Config{}, fmt.Errorf("-public-url: %w", err)
		}
		cfg.PublicURL = u
	}
	if cfg.ReadOnly && len(cfg.AllowRecipients) > 0 {
		return Config{}, errors.New("-allow-recipients has no effect with -read-only; set one or the other")
	}
	if cfg.ReadOnly && cfg.EnableGroupAdmin {
		return Config{}, errors.New("-enable-group-admin has no effect with -read-only; set one or the other")
	}
	return cfg, nil
}

// publicURL validates and normalises the public origin: https, or http for a
// loopback host (for tests and local proxies), with no path, query,
// fragment or credentials.
func publicURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case scheme != "https" && scheme != "http", u.Host == "":
		return "", fmt.Errorf("%q is not an absolute http(s) URL such as https://wa.example.com", raw)
	case u.User != nil:
		return "", fmt.Errorf("%q must not contain credentials", raw)
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return "", fmt.Errorf("%q must not have a query or fragment", raw)
	case u.Path != "" && u.Path != "/":
		return "", fmt.Errorf("%q has a path; serve the server at the root of its own (sub)domain", raw)
	case scheme == "http" && !httpx.IsLoopback(u.Hostname()):
		return "", fmt.Errorf("%q is plain http; put the server behind a TLS reverse proxy and use https", raw)
	}
	return scheme + "://" + strings.ToLower(u.Host), nil
}

// envBool reads a true/false environment variable; unset is false.
func envBool(getenv func(string) string, name string) (bool, error) {
	v := getenv(name)
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", name, err)
	}
	return b, nil
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
