// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func testPoll() *waE2E.PollCreationMessage {
	return &waE2E.PollCreationMessage{
		Name: proto.String("Where shall we eat?"),
		Options: []*waE2E.PollCreationMessage_Option{
			{OptionName: proto.String("Pizza")},
			{OptionName: proto.String("Sushi")},
		},
		SelectableOptionsCount: proto.Uint32(1),
	}
}

const testPollText = "Where shall we eat?\n- Pizza\n- Sushi"

// Every poll message version is archived the same way: the question and
// options as text, the poll itself as the ref.
func TestPollVersions(t *testing.T) {
	poll := testPoll()
	tests := []struct {
		name string
		msg  *waE2E.Message
	}{
		{"v1", &waE2E.Message{PollCreationMessage: poll}},
		{"v2", &waE2E.Message{PollCreationMessageV2: poll}},
		{"v3", &waE2E.Message{PollCreationMessageV3: poll}},
		{"v4", &waE2E.Message{PollCreationMessageV4: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{PollCreationMessage: poll},
		}}},
		{"v5", &waE2E.Message{PollCreationMessageV5: poll}},
		{"v6", &waE2E.Message{PollCreationMessageV6: poll}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, kind := messageContent(tt.msg)
			if text != testPollText || kind != "poll" {
				t.Errorf("messageContent = %q, %q; want %q, poll", text, kind, testPollText)
			}
			for _, fromMe := range []bool{false, true} {
				var got waE2E.PollCreationMessage
				if err := proto.Unmarshal(mediaRef(tt.msg, fromMe), &got); err != nil {
					t.Fatalf("fromMe=%v: decode ref: %v", fromMe, err)
				}
				if !proto.Equal(&got, poll) {
					t.Errorf("fromMe=%v: ref = %v, want %v", fromMe, &got, poll)
				}
			}
		})
	}
	if pollCreation(&waE2E.Message{Conversation: proto.String("hi")}) != nil {
		t.Error("a text message is not a poll")
	}
	if pollCreation(&waE2E.Message{PollCreationMessageV4: &waE2E.FutureProofMessage{}}) != nil {
		t.Error("an empty V4 wrapper is not a poll")
	}
}

func TestToMessageArchivesPolls(t *testing.T) {
	chat := types.NewJID("120363000000000000", types.GroupServer)
	e := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: types.NewJID("31600000001", types.DefaultUserServer), IsGroup: true},
			ID:            "POLL", Timestamp: time.Unix(1_700_000_000, 0),
		},
		Message: &waE2E.Message{PollCreationMessageV3: testPoll()},
	}
	m, ok := toMessage(e, types.JID.ToNonAD)
	if !ok {
		t.Fatal("toMessage: a poll should be archived")
	}
	if m.MediaType != "poll" || m.Text != testPollText || len(m.MediaRef) == 0 {
		t.Errorf("toMessage = %+v", m)
	}
}

// A poll with an empty question still has its options to show and search.
func TestPollTextWithoutQuestion(t *testing.T) {
	p := testPoll()
	p.Name = nil
	if got := pollText(p); got != "\n- Pizza\n- Sushi" {
		t.Errorf("pollText = %q", got)
	}
}

func TestPollsHaveNoMediaAndCannotBeEdited(t *testing.T) {
	poll := store.Message{MessageID: "POLL", MediaType: "poll", FromMe: true, Text: testPollText,
		MediaRef: mediaRef(&waE2E.Message{PollCreationMessage: testPoll()}, true), Timestamp: time.Now().Unix()}
	if _, err := (&Client{}).Media(context.Background(), poll); err == nil || !strings.Contains(err.Error(), "poll") {
		t.Errorf("Media(poll) error = %v, want one naming the poll", err)
	}
	if err := editable(poll, "new question", time.Now()); err == nil || !strings.Contains(err.Error(), "poll") {
		t.Errorf("editable(poll) error = %v, want one naming the poll", err)
	}
}
