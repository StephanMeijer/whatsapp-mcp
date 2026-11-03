// SPDX-License-Identifier: MPL-2.0

package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllowHosts(t *testing.T) {
	h := AllowHosts([]string{"WA.example.com"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	tests := []struct {
		host string
		want int
	}{
		{"wa.example.com", http.StatusNoContent},
		{"WA.Example.COM:443", http.StatusNoContent},
		{"127.0.0.1:8080", http.StatusNoContent}, // nginx's default Host, or a local client
		{"localhost:8080", http.StatusNoContent},
		{"[::1]:8080", http.StatusNoContent},
		{"evil.example.com", http.StatusForbidden},
		{"wa.example.com.evil.net", http.StatusForbidden},
		{"192.168.1.10:8080", http.StatusForbidden},
		{"", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			r.Host = tt.host
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Errorf("Host %q: status %d, want %d", tt.host, w.Code, tt.want)
			}
		})
	}
}
