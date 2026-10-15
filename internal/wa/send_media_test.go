// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"bytes"
	"testing"

	"go.mau.fi/whatsmeow"
)

var (
	jpegHeader = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0}
	pngHeader  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")
	oggHeader  = []byte("OggS\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00")
)

func TestDetectMIME(t *testing.T) {
	tests := []struct {
		kind    string
		data    []byte
		hint    string
		want    string
		wantErr bool
	}{
		{kind: "image", data: jpegHeader, want: "image/jpeg"},
		{kind: "image", data: pngHeader, want: "image/png"},
		{kind: "image", data: []byte("just text"), wantErr: true},
		{kind: "image", data: []byte("x"), hint: "image/png; charset=binary", want: "image/png"},
		{kind: "image", data: jpegHeader, hint: "image/gif", wantErr: true},
		{kind: "audio", data: oggHeader, want: "audio/ogg"},
		{kind: "video", data: jpegHeader, wantErr: true},
		{kind: "document", data: []byte("plain notes"), want: "text/plain"},
		{kind: "document", data: jpegHeader, want: "image/jpeg"},
		{kind: "hologram", data: jpegHeader, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.kind+"/"+tt.want+tt.hint, func(t *testing.T) {
			got, err := detectMIME(tt.kind, tt.data, tt.hint)
			if (err != nil) != tt.wantErr || (!tt.wantErr && got != tt.want) {
				t.Errorf("detectMIME = %q, %v; want %q, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestBuildMediaMessage(t *testing.T) {
	up := whatsmeow.UploadResponse{
		URL: "https://mmg/x", DirectPath: "/v/x", MediaKey: []byte("key"),
		FileEncSHA256: []byte("enc"), FileSHA256: []byte("sha"), FileLength: 42,
	}
	for _, kind := range []string{"image", "video", "audio", "document"} {
		t.Run(kind, func(t *testing.T) {
			msg := buildMediaMessage(kind, up, "x/y", "a caption", "notes.txt")
			text, gotKind := messageContent(msg)
			if gotKind != kind {
				t.Fatalf("kind = %q", gotKind)
			}
			sub, fileName, err := decodeMediaRef(kind, mediaRef(msg, true))
			if err != nil {
				t.Fatal(err)
			}
			if sub.GetDirectPath() != "/v/x" || !bytes.Equal(sub.GetMediaKey(), []byte("key")) ||
				!bytes.Equal(sub.GetFileSHA256(), []byte("sha")) || sub.GetFileLength() != 42 || sub.GetMimetype() != "x/y" {
				t.Errorf("sub-message = %+v", sub)
			}
			switch kind {
			case "audio":
				if text != "" {
					t.Errorf("audio has text %q", text)
				}
			case "document":
				if text != "a caption" || fileName != "notes.txt" {
					t.Errorf("document text %q file %q", text, fileName)
				}
			default:
				if text != "a caption" {
					t.Errorf("caption = %q", text)
				}
			}
		})
	}
	if msg := buildMediaMessage("image", up, "image/png", "", ""); msg.GetImageMessage().Caption != nil {
		t.Error("empty caption should be omitted")
	}
}
