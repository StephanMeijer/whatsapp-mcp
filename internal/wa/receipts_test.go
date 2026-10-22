// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"testing"

	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestReceiptStatus(t *testing.T) {
	tests := []struct {
		in   types.ReceiptType
		want string // empty: ignored
	}{
		{types.ReceiptTypeDelivered, store.StatusDelivered},
		{types.ReceiptTypeRead, store.StatusRead},
		{types.ReceiptTypePlayed, store.StatusPlayed},
		{types.ReceiptTypeSender, ""},
		{types.ReceiptTypeRetry, ""},
		{types.ReceiptTypeReadSelf, ""},
		{types.ReceiptTypePlayedSelf, ""},
		{types.ReceiptTypeInactive, ""},
		{types.ReceiptTypeHistorySync, ""},
	}
	for _, tt := range tests {
		got, ok := receiptStatus(tt.in)
		if ok != (tt.want != "") || got != tt.want {
			t.Errorf("receiptStatus(%q) = %q, %v; want %q", tt.in, got, ok, tt.want)
		}
	}
}
