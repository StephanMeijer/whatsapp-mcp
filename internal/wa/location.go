// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// locationText is the archived text of a shared location: its name and
// address (searchable), then the coordinates and a map link.
func locationText(name, address string, lat, lon float64) string {
	coords := strconv.FormatFloat(lat, 'f', -1, 64) + "," + strconv.FormatFloat(lon, 'f', -1, 64)
	var lines []string
	for _, s := range []string{name, address} {
		if s = strings.TrimSpace(s); s != "" {
			lines = append(lines, s)
		}
	}
	return strings.Join(append(lines, coords+" https://maps.google.com/?q="+coords), "\n")
}

// locationContent returns the archived text of a location or live location
// message, or false if m is neither. A live location is archived at the
// point it was shared from.
func locationContent(m *waE2E.Message) (string, bool) {
	if l := m.GetLocationMessage(); l != nil {
		return locationText(l.GetName(), l.GetAddress(), l.GetDegreesLatitude(), l.GetDegreesLongitude()), true
	}
	if l := m.GetLiveLocationMessage(); l != nil {
		return locationText(l.GetCaption(), "", l.GetDegreesLatitude(), l.GetDegreesLongitude()), true
	}
	return "", false
}

// SendLocation sends a location pin, with an optional name and address, and
// archives it.
func (c *Client) SendLocation(ctx context.Context, recipient string, lat, lon float64, name, address string) (store.Message, error) {
	if math.IsNaN(lat) || lat < -90 || lat > 90 {
		return store.Message{}, fmt.Errorf("latitude must be between -90 and 90, not %v", lat)
	}
	if math.IsNaN(lon) || lon < -180 || lon > 180 {
		return store.Message{}, fmt.Errorf("longitude must be between -180 and 180, not %v", lon)
	}
	to, err := c.recipient(recipient)
	if err != nil {
		return store.Message{}, err
	}
	loc := &waE2E.LocationMessage{DegreesLatitude: proto.Float64(lat), DegreesLongitude: proto.Float64(lon)}
	if name != "" {
		loc.Name = proto.String(name)
	}
	if address != "" {
		loc.Address = proto.String(address)
	}
	return c.sendAndArchive(ctx, to, &waE2E.Message{LocationMessage: loc}, "location")
}
