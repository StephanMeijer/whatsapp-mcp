// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

// mediaDir returns a media folder holding photo.jpg, plus a secret file next
// to it and a symlink inside it pointing at that secret.
func mediaDir(t *testing.T) (dir, secret string) {
	t.Helper()
	base := t.TempDir()
	dir = filepath.Join(base, "media")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("jpeg bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret = filepath.Join(base, "secret.txt")
	if err := os.WriteFile(secret, []byte("do not send"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	return dir, secret
}

func TestSendMediaData(t *testing.T) {
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{})
	file := []byte{0xFF, 0xD8, 0xFF, 0x00, 0x01, 0x02}
	var out Message
	call(t, cs, "send_media", map[string]any{
		"recipient": "+31600000001", "kind": "image", "data": base64.StdEncoding.EncodeToString(file), "caption": "beach",
	}, &out)
	if out.ID != "MEDIA1" || out.MediaType != "image" {
		t.Errorf("out = %+v", out)
	}
	if !bytes.Equal(fake.lastMedia, file) {
		t.Errorf("file arrived as %v, want %v", fake.lastMedia, file)
	}
	if want := []string{`media 31600000001@s.whatsapp.net image "" 6 bytes "beach"`}; !slices.Equal(fake.sent, want) {
		t.Errorf("sent = %q, want %q", fake.sent, want)
	}
}

func TestSendMediaPath(t *testing.T) {
	dir, secret := mediaDir(t)
	fake := &fakeWA{}
	cs := connect(t, fake, storetest.New(t), Options{MediaDir: dir})

	call(t, cs, "send_media", map[string]any{"recipient": "+31600000001", "kind": "document", "path": "photo.jpg"}, nil)
	if want := []string{`media 31600000001@s.whatsapp.net document "photo.jpg" 10 bytes ""`}; !slices.Equal(fake.sent, want) {
		t.Fatalf("sent = %q, want %q", fake.sent, want)
	}

	for _, path := range []string{"../secret.txt", secret, "link.txt", "missing.jpg"} {
		if res := call(t, cs, "send_media", map[string]any{"recipient": "+31600000001", "kind": "document", "path": path}, nil); !res.IsError {
			t.Errorf("path %q: want tool error", path)
		}
	}
	if len(fake.sent) != 1 {
		t.Errorf("a refused path was still sent: %q", fake.sent)
	}
}

func TestSendMediaErrors(t *testing.T) {
	dir, _ := mediaDir(t)
	fake := &fakeWA{}
	noDir := connect(t, fake, storetest.New(t), Options{})
	withDir := connect(t, fake, storetest.New(t), Options{MediaDir: dir})
	allowlisted := connect(t, fake, storetest.New(t), Options{AllowRecipients: []string{"+31600000001"}})
	// One byte over the limit, sent as a file so the test doesn't push 21 MB of JSON through the pipe.
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), make([]byte, wa.MaxMediaBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name string
		cs   *mcp.ClientSession
		args map[string]any
	}{
		{"path without media dir", noDir, map[string]any{"recipient": "+31600000001", "kind": "image", "path": "photo.jpg"}},
		{"data and path", withDir, map[string]any{"recipient": "+31600000001", "kind": "image", "path": "photo.jpg", "data": "AQ=="}},
		{"no file", noDir, map[string]any{"recipient": "+31600000001", "kind": "image"}},
		{"no kind", noDir, map[string]any{"recipient": "+31600000001", "data": "AQ=="}},
		{"not base64", noDir, map[string]any{"recipient": "+31600000001", "kind": "image", "data": "not base64!"}},
		{"too big", withDir, map[string]any{"recipient": "+31600000001", "kind": "document", "path": "big.bin"}},
		{"outside allowlist", allowlisted, map[string]any{"recipient": "+31600000002", "kind": "image", "data": "AQ=="}},
	} {
		if res := call(t, c.cs, "send_media", c.args, nil); !res.IsError {
			t.Errorf("%s: want tool error", c.name)
		}
	}
	if len(fake.sent) != 0 {
		t.Errorf("failed calls still sent: %q", fake.sent)
	}
}

func TestDecodeBase64(t *testing.T) {
	for _, in := range []string{"AQID", "AQI=", "AQI", " AQID\n"} {
		if _, err := decodeBase64(in); err != nil {
			t.Errorf("decodeBase64(%q): %v", in, err)
		}
	}
	if _, err := decodeBase64("***"); err == nil {
		t.Error("invalid base64: want error")
	}
}

// The HTTP transport must accept a request carrying the largest allowed
// file; the SDK default (4 MiB) would not.
func TestHandlerAcceptsLargeRequests(t *testing.T) {
	if maxRequestBody < int64(base64.StdEncoding.EncodedLen(wa.MaxMediaBytes)) {
		t.Fatalf("maxRequestBody %d cannot carry a %d byte file", maxRequestBody, wa.MaxMediaBytes)
	}
	srv, err := New("test", &fakeWA{}, storetest.New(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(srv)
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},` +
		`"clientInfo":{"name":"t","version":"0"},"padding":"` + strings.Repeat("x", 6<<20) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("6 MiB request rejected as too large: %s", rec.Body.String())
	}
}

func TestInvalidMediaDir(t *testing.T) {
	if _, err := New("test", &fakeWA{}, storetest.New(t), Options{MediaDir: filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Error("missing media dir: want error")
	}
}
