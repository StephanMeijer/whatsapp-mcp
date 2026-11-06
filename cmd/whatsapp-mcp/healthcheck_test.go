// SPDX-License-Identifier: MPL-2.0

package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

func TestHealthURL(t *testing.T) {
	tests := []struct{ addr, want string }{
		{"", "http://127.0.0.1:8080/healthz"},
		{"127.0.0.1:9000", "http://127.0.0.1:9000/healthz"},
		{"0.0.0.0:8080", "http://127.0.0.1:8080/healthz"},
		{":8080", "http://127.0.0.1:8080/healthz"},
		{"[::]:8080", "http://127.0.0.1:8080/healthz"},
		{"[::1]:8080", "http://[::1]:8080/healthz"},
		{"localhost:8080", "http://localhost:8080/healthz"},
	}
	for _, tt := range tests {
		got, err := healthURL(tt.addr)
		if err != nil || got != tt.want {
			t.Errorf("healthURL(%q) = %q, %v; want %q", tt.addr, got, err, tt.want)
		}
	}
	if _, err := healthURL("8080"); err == nil {
		t.Error("an address without a port: want an error")
	}
}

func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	addr := srv.Listener.Addr().String()

	if err := healthcheck(addr); err != nil {
		t.Errorf("healthy server: %v", err)
	}
	status = http.StatusServiceUnavailable
	if err := healthcheck(addr); err == nil {
		t.Error("HTTP 503: want an error")
	}

	// A port nothing listens on.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := l.Addr().String()
	l.Close()
	if err := healthcheck(closed); err == nil {
		t.Error("no server: want an error")
	}
}

type statusFunc func() wa.Status

func (f statusFunc) Status() wa.Status { return f() }

func TestHealthHandler(t *testing.T) {
	const jid = "31612345678@s.whatsapp.net"
	tests := []struct {
		name   string
		status wa.Status
		want   int
	}{
		{"logged in", wa.Status{Connected: true, LoggedIn: true, JID: jid}, http.StatusOK},
		{"waiting for a QR scan", wa.Status{Connected: true, Pairing: true}, http.StatusOK},
		{"logged out or pairing ended", wa.Status{Connected: false, JID: jid}, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			healthHandler(statusFunc(func() wa.Status { return tt.status })).
				ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if rec.Code != tt.want {
				t.Errorf("status %d, want %d", rec.Code, tt.want)
			}
			if strings.Contains(rec.Body.String(), "31612345678") {
				t.Errorf("body %q names the account", rec.Body.String())
			}
		})
	}
}
