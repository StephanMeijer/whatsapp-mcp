// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"math"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestMessageContentLocations(t *testing.T) {
	tests := []struct {
		name string
		msg  *waE2E.Message
		text string
	}{
		{"named place", &waE2E.Message{LocationMessage: &waE2E.LocationMessage{
			DegreesLatitude: proto.Float64(52.3731), DegreesLongitude: proto.Float64(4.8926),
			Name: proto.String("Dam Square"), Address: proto.String("Dam, Amsterdam"),
		}}, "Dam Square\nDam, Amsterdam\n52.3731,4.8926 https://maps.google.com/?q=52.3731,4.8926"},
		{"bare pin", &waE2E.Message{LocationMessage: &waE2E.LocationMessage{
			DegreesLatitude: proto.Float64(-33.8568), DegreesLongitude: proto.Float64(151.2153),
		}}, "-33.8568,151.2153 https://maps.google.com/?q=-33.8568,151.2153"},
		{"live location", &waE2E.Message{LiveLocationMessage: &waE2E.LiveLocationMessage{
			DegreesLatitude: proto.Float64(1.5), DegreesLongitude: proto.Float64(2), Caption: proto.String("on my way"),
		}}, "on my way\n1.5,2 https://maps.google.com/?q=1.5,2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, kind := messageContent(tt.msg)
			if text != tt.text || kind != store.MediaLocation {
				t.Errorf("messageContent = %q, %q; want %q, location", text, kind, tt.text)
			}
		})
	}
}

func TestSendLocation(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	m, err := c.SendLocation(ctx, "+31600000001", 52.37, 4.89, "Dam", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 1 {
		t.Fatalf("sent %d messages", len(fake.sent))
	}
	loc := fake.sent[0].msg.GetLocationMessage()
	if loc.GetDegreesLatitude() != 52.37 || loc.GetDegreesLongitude() != 4.89 || loc.GetName() != "Dam" || loc.Address != nil {
		t.Errorf("sent %+v", loc)
	}
	if m.MediaType != store.MediaLocation || m.Text != "Dam\n52.37,4.89 https://maps.google.com/?q=52.37,4.89" {
		t.Errorf("archived %+v", m)
	}
	got, err := c.store.MessageByID(ctx, m.ChatJID, m.MessageID)
	if err != nil || got.Text != m.Text {
		t.Errorf("archive lookup = %+v, %v", got, err)
	}
}

func TestSendLocationRejectsBadCoordinates(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	for _, ll := range [][2]float64{{91, 0}, {-91, 0}, {0, 181}, {0, -181}, {math.NaN(), 0}, {0, math.NaN()}} {
		if _, err := c.SendLocation(context.Background(), "+31600000001", ll[0], ll[1], "", ""); err == nil {
			t.Errorf("SendLocation(%v): want error", ll)
		}
	}
	if len(fake.sent) != 0 {
		t.Errorf("bad coordinates still sent: %d", len(fake.sent))
	}
}

// A location has no file to download and no caption to edit.
func TestLocationHasNoMediaOrEdit(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	m := store.Message{MessageID: "L1", FromMe: true, MediaType: store.MediaLocation, Timestamp: sendTime.Unix()}
	if _, err := c.Media(context.Background(), m); err == nil {
		t.Error("Media: want error for a location")
	}
	if err := editable(m, "new", sendTime); err == nil {
		t.Error("editable: want error for a location")
	}
}
