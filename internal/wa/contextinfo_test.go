// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func fwd(score uint32) *waE2E.ContextInfo {
	return &waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(score)}
}

func TestForwarding(t *testing.T) {
	tests := []struct {
		name      string
		msg       *waE2E.Message
		forwarded bool
		score     int
	}{
		{"nil message", nil, false, 0},
		{"plain conversation has no context", &waE2E.Message{Conversation: proto.String("hi")}, false, 0},
		{"extended text, forwarded", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("hi"), ContextInfo: fwd(1)}}, true, 1},
		{"image, forwarded many times", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{ContextInfo: fwd(7)}}, true, 7},
		{"document with a context but not forwarded", &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("q")}}}, false, 0},
		{"media without a context", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{}}, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forwarded, score := forwarding(tt.msg)
			if forwarded != tt.forwarded || score != tt.score {
				t.Errorf("forwarding = %v, %d; want %v, %d", forwarded, score, tt.forwarded, tt.score)
			}
		})
	}
}

func TestToMessageRecordsForwarding(t *testing.T) {
	e := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: alicePN, Sender: alicePN},
			ID:            "F1",
			Timestamp:     time.Unix(100, 0),
		},
		Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("chain letter"), ContextInfo: fwd(5)}},
	}
	m, ok := toMessage(e, func(j types.JID) types.JID { return j })
	if !ok || !m.Forwarded || m.ForwardingScore != 5 {
		t.Errorf("toMessage = %+v, %v; want forwarded with score 5", m, ok)
	}
}
