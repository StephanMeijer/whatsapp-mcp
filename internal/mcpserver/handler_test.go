// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

// A reverse proxy on the same machine that keeps the client's Host (Caddy,
// Tailscale Serve) reaches a loopback listener with a public Host. The SDK
// refuses that as DNS rebinding unless the check is handed to the caller.
func TestHandlerBehindProxy(t *testing.T) {
	srv, err := New("test", &fakeWA{}, storetest.New(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	initialize := func(h http.Handler) int {
		ts := httptest.NewServer(h) // listens on loopback
		defer ts.Close()
		body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18",` +
			`"capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = "wa.example.com"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := initialize(Handler(srv, HandlerOptions{})); got != http.StatusForbidden {
		t.Errorf("default: status %d, want 403 from the SDK's Host check", got)
	}
	if got := initialize(Handler(srv, HandlerOptions{HostCheckedByCaller: true})); got != http.StatusOK {
		t.Errorf("host checked by caller: status %d, want 200", got)
	}
}
