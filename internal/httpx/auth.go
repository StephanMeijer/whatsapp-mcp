// SPDX-License-Identifier: MPL-2.0

// Package httpx holds HTTP middleware.
package httpx

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// RequireBearer rejects requests whose Authorization header does not carry token.
func RequireBearer(token string, next http.Handler) http.Handler {
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="whatsapp-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
