// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"testing"

	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestReplyMessage(t *testing.T) {
	sender := types.NewJID("31600000001", types.DefaultUserServer)
	msg := replyMessage("sounds good", store.Message{MessageID: "ORIG", Text: "dinner at 8?"}, sender)
	ext := msg.GetExtendedTextMessage()
	ci := ext.GetContextInfo()
	if ext.GetText() != "sounds good" || ci.GetStanzaID() != "ORIG" || ci.GetParticipant() != sender.String() ||
		ci.GetQuotedMessage().GetConversation() != "dinner at 8?" {
		t.Errorf("reply = %v", msg)
	}

	media := replyMessage("nice", store.Message{MessageID: "PIC", MediaType: "image"}, sender)
	if got := media.GetExtendedTextMessage().GetContextInfo().GetQuotedMessage().GetConversation(); got != "[image]" {
		t.Errorf("quote of a captionless image = %q, want [image]", got)
	}
}

func TestChooseWireSender(t *testing.T) {
	own := types.NewJID("31600000000", types.DefaultUserServer)
	pn := types.NewJID("31600000001", types.DefaultUserServer)
	lid := types.NewJID("111", types.HiddenUserServer)
	dm := pn
	group := types.NewJID("120363000000000000", types.GroupServer)

	tests := []struct {
		name      string
		chat      types.JID
		fromMe    bool
		lidGroup  bool
		senderLID types.JID
		want      types.JID
	}{
		{"own message", group, true, true, lid, own},
		{"direct chat", dm, false, false, types.JID{}, pn},
		{"phone-number group", group, false, false, lid, pn},
		{"LID group with known LID", group, false, true, lid, lid},
		{"LID group without known LID", group, false, true, types.JID{}, pn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chooseWireSender(tt.chat, pn, own, tt.fromMe, tt.lidGroup, tt.senderLID); got != tt.want {
				t.Errorf("chooseWireSender = %s, want %s", got, tt.want)
			}
		})
	}
}
