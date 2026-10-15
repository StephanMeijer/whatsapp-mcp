// SPDX-License-Identifier: MPL-2.0

package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireBearer(t *testing.T) {
	h := RequireBearer("s3cret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	tests := []struct {
		header string
		want   int
	}{
		{"Bearer s3cret", http.StatusNoContent},
		{"", http.StatusUnauthorized},
		{"Bearer wrong", http.StatusUnauthorized},
		{"Bearer s3cret2", http.StatusUnauthorized},
		{"Basic s3cret", http.StatusUnauthorized},
		{"s3cret", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Errorf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}
