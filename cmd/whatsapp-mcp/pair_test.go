// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

// qrCode is a pairing state: the code waiting to be scanned, or "" if none.
type qrCode string

func (q qrCode) QRCode() string { return string(q) }

func TestPairHandler(t *testing.T) {
	t.Run("no pairing in progress", func(t *testing.T) {
		rec := httptest.NewRecorder()
		pairHandler(qrCode("")).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pair", nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("status %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("pending code is served as an uncached PNG", func(t *testing.T) {
		rec := httptest.NewRecorder()
		pairHandler(qrCode("2@abc,def,ghi")).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pair", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d, want %d", rec.Code, http.StatusOK)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
			t.Errorf("Content-Type %q, want image/png", ct)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control %q, want no-store", cc)
		}
		img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatalf("body is not a PNG: %v", err)
		}
		if w := img.Bounds().Dx(); w != 512 {
			t.Errorf("image width %d, want 512", w)
		}
	})
}
