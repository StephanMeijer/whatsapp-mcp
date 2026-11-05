// SPDX-License-Identifier: MPL-2.0

package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
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
