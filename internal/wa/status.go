// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// PostStatus posts a text status update to the audience set in the phone's
// status privacy settings, which whatsmeow looks up, and archives it as the
// account's own. Media statuses go through SendMedia with
// types.StatusBroadcastJID as the recipient.
func (c *Client) PostStatus(ctx context.Context, text string) (store.Message, error) {
	if !c.cli.IsLoggedIn() {
		return store.Message{}, errNotLoggedIn
	}
	// Text statuses are extended text messages; a plain conversation isn't shown as one.
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text)}}
	return c.sendAndArchive(ctx, types.StatusBroadcastJID, msg, "status")
}
