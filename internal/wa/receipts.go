// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// receiptStatus maps a receipt type to a message status. Only what a
// recipient did with a message counts; receipts from the account's own
// devices, retries and the like are ignored.
func receiptStatus(t types.ReceiptType) (string, bool) {
	switch t {
	case types.ReceiptTypeDelivered:
		return store.StatusDelivered, true
	case types.ReceiptTypeRead:
		return store.StatusRead, true
	case types.ReceiptTypePlayed:
		return store.StatusPlayed, true
	}
	return "", false
}

// onReceipt records what a recipient did with messages the account sent.
func (c *Client) onReceipt(e *events.Receipt) {
	status, ok := receiptStatus(e.Type)
	if !ok {
		return
	}
	// A receipt naming another sender is the account reading that person's
	// message on another device, not a recipient reading ours.
	if !e.MessageSender.IsEmpty() && !c.isSelf(e.MessageSender) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
	defer cancel()
	chat := c.Canonical(ctx, e.Chat).String()
	participant := c.Canonical(ctx, e.Sender).String()
	for _, id := range e.MessageIDs {
		moved, err := c.store.RecordReceipt(ctx, chat, id, participant, status, e.Timestamp)
		if err != nil {
			c.log.Error("record receipt", "err", err, "chat", chat, "id", id)
			continue
		}
		c.log.Debug("receipt", "chat", chat, "id", id, "from", participant, "status", status, "moved", moved)
	}
}

// isSelf reports whether jid is the account, by phone number or LID.
func (c *Client) isSelf(jid types.JID) bool {
	jid = jid.ToNonAD()
	if id := c.cli.Store.ID; id != nil && id.ToNonAD() == jid {
		return true
	}
	if lid := c.cli.Store.GetLID(); !lid.IsEmpty() && lid.ToNonAD() == jid {
		return true
	}
	return false
}
