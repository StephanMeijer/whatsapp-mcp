// SPDX-License-Identifier: MPL-2.0

// Command whatsapp-mcp is an MCP server, served over streamable HTTP, for a
// WhatsApp account linked as a companion device.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skip2/go-qrcode"

	"github.com/StephanMeijer/whatsapp-mcp/internal/config"
	"github.com/StephanMeijer/whatsapp-mcp/internal/db"
	"github.com/StephanMeijer/whatsapp-mcp/internal/httpx"
	"github.com/StephanMeijer/whatsapp-mcp/internal/mcpserver"
	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "whatsapp-mcp:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if err != nil {
		return err
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return fmt.Errorf("log level: %w", err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()

	st, err := store.New(ctx, database)
	if err != nil {
		return err
	}
	client, err := wa.New(ctx, database.SQL, database.Dialect, st, log, os.Stderr)
	if err != nil {
		return err
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("connect to WhatsApp: %w", err)
	}
	defer client.Stop()

	mcpSrv, err := mcpserver.New(version, client, st, mcpserver.Options{
		ReadOnly:        cfg.ReadOnly,
		AllowRecipients: cfg.AllowRecipients,
		Logger:          log,
		MediaDir:        cfg.MediaDir,
	})
	if err != nil {
		return err
	}
	handler, err := routes(cfg.PublicURL, cfg.AuthToken, mcpSrv, pairHandler(client))
	if err != nil {
		return err
	}
	mcpURL := "http://" + cfg.Addr + "/mcp"
	if cfg.PublicURL != "" {
		mcpURL = cfg.PublicURL + "/mcp"
	}

	// Cancelled when shutdown starts, so long-lived MCP streams end instead of
	// holding Shutdown open until its deadline.
	baseCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("serving MCP", "url", mcpURL, "listen", cfg.Addr, "version", version,
			"read_only", cfg.ReadOnly, "allowed_recipients", len(cfg.AllowRecipients))
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	cancelRequests()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Warn("graceful shutdown incomplete, closing remaining connections", "err", err)
		return srv.Close()
	}
	return nil
}

// routes serves health, the pairing code and MCP. With a public URL the
// server sits behind a reverse proxy, which passes on the public Host to a
// loopback listener; the SDK's DNS-rebinding check would refuse that, so
// AllowHosts takes over the check, for every endpoint, accepting the public
// host and loopback.
func routes(publicURL, token string, mcpSrv *mcp.Server, pair http.Handler) (http.Handler, error) {
	behindProxy := publicURL != ""
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /pair", httpx.RequireBearer(token, pair))
	mux.Handle("/mcp", httpx.RequireBearer(token,
		mcpserver.Handler(mcpSrv, mcpserver.HandlerOptions{HostCheckedByCaller: behindProxy})))
	if !behindProxy {
		return mux, nil
	}
	public, err := url.Parse(publicURL)
	if err != nil {
		return nil, fmt.Errorf("public URL: %w", err)
	}
	return httpx.AllowHosts([]string{public.Hostname()}, mux), nil
}

// pairHandler serves the pending pairing QR code as a PNG, for hosts where
// the terminal output isn't visible.
func pairHandler(client interface{ QRCode() string }) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		code := client.QRCode()
		if code == "" {
			http.Error(w, "no pairing in progress", http.StatusNotFound)
			return
		}
		png, err := qrcode.Encode(code, qrcode.Medium, 512)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(png)
	})
}
