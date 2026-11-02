// SPDX-License-Identifier: MPL-2.0

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testToken = "0123456789abcdef0123456789abcdef"

// The full HTTP stack on a loopback listener, as a reverse proxy on the same
// machine reaches it: which Host headers get through to MCP.
func TestRoutesHostCheck(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	pair := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	tests := []struct {
		name, publicURL, host, token, path string
		want                               int
	}{
		{"no public url: proxy keeping the public host is refused", "", "wa.example.com", testToken, "/mcp", http.StatusForbidden},
		{"no public url: loopback host works", "", "127.0.0.1", testToken, "/mcp", http.StatusOK},
		{"public url: its host gets through", "https://wa.example.com", "wa.example.com", testToken, "/mcp", http.StatusOK},
		{"public url: host with a port", "https://wa.example.com", "wa.example.com:443", testToken, "/mcp", http.StatusOK},
		{"public url: loopback host still works", "https://wa.example.com", "127.0.0.1:8080", testToken, "/mcp", http.StatusOK},
		{"public url: another host is refused", "https://wa.example.com", "evil.example.com", testToken, "/mcp", http.StatusForbidden},
		{"public url: token still required", "https://wa.example.com", "wa.example.com", "", "/mcp", http.StatusUnauthorized},
		{"public url: health on loopback", "https://wa.example.com", "127.0.0.1:8080", "", "/healthz", http.StatusOK},
		{"public url: pairing needs the token", "https://wa.example.com", "wa.example.com", "", "/pair", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := routes(tt.publicURL, testToken, srv, pair)
			if err != nil {
				t.Fatal(err)
			}
			ts := httptest.NewServer(h)
			defer ts.Close()
			method, body := http.MethodGet, ""
			if tt.path == "/mcp" {
				method = http.MethodPost
				body = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18",` +
					`"capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
			}
			req, err := http.NewRequest(method, ts.URL+tt.path, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Host = tt.host
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Errorf("status %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}
