// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waVnameCert"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestContactInfo(t *testing.T) {
	jid := types.NewJID("31600000001", types.DefaultUserServer)
	business := &types.VerifiedName{Details: &waVnameCert.VerifiedNameCertificate_Details{VerifiedName: proto.String("Acme BV")}}
	got := contactInfo(jid, types.UserInfo{Status: "Out of office", PictureID: "123", VerifiedName: business}, "Alice")
	want := ContactInfo{JID: "31600000001@s.whatsapp.net", Name: "Alice", About: "Out of office", BusinessName: "Acme BV", HasPicture: true}
	if got != want {
		t.Errorf("contactInfo = %+v, want %+v", got, want)
	}
	if got := contactInfo(jid, types.UserInfo{}, ""); got != (ContactInfo{JID: "31600000001@s.whatsapp.net"}) {
		t.Errorf("contactInfo of nothing = %+v", got)
	}
}

func TestFetchPicture(t *testing.T) {
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte("x"), 100)...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pic.jpg":
			w.Write(jpeg)
		case "/huge":
			w.Write(bytes.Repeat([]byte("x"), MaxMediaBytes+1))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, err := fetchPicture(context.Background(), srv.URL+"/pic.jpg")
	if err != nil {
		t.Fatalf("fetchPicture: %v", err)
	}
	if got.Kind != "image" || got.MIMEType != "image/jpeg" || !bytes.Equal(got.Data, jpeg) {
		t.Errorf("fetchPicture = kind %q mime %q %d bytes", got.Kind, got.MIMEType, len(got.Data))
	}
	if _, err := fetchPicture(context.Background(), srv.URL+"/huge"); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("huge picture: err = %v, want the size limit", err)
	}
	if _, err := fetchPicture(context.Background(), srv.URL+"/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("missing picture: err = %v, want HTTP 404", err)
	}
}
