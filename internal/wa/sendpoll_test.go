// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestSendPoll(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	ctx := context.Background()
	m, err := c.SendPoll(ctx, bobPN.String(), " Lunch? ", []string{"Pizza", " Sushi "}, false)
	if err != nil {
		t.Fatal(err)
	}
	p := fake.sent[0].msg.GetPollCreationMessage()
	if p.GetName() != "Lunch?" || len(p.GetOptions()) != 2 || p.GetOptions()[1].GetOptionName() != "Sushi" || p.GetSelectableOptionsCount() != 1 {
		t.Errorf("sent poll = %v", p)
	}
	if len(fake.sent[0].msg.GetMessageContextInfo().GetMessageSecret()) == 0 {
		t.Error("the poll has no message secret, so votes on it could not be decrypted")
	}
	// Archived as a poll whose options a later vote tally can read.
	a := archived(t, c, bobPN, m.MessageID)
	var ref waE2E.PollCreationMessage
	if a.MediaType != store.MediaPoll || proto.Unmarshal(a.MediaRef, &ref) != nil || len(ref.GetOptions()) != 2 {
		t.Errorf("archived = %+v", a)
	}
	if _, err := TallyPoll(a, nil); err != nil {
		t.Errorf("tally the sent poll: %v", err)
	}

	if _, err := c.SendPoll(ctx, bobPN.String(), "Plans?", []string{"A", "B", "C"}, true); err != nil {
		t.Fatal(err)
	}
	if n := fake.sent[1].msg.GetPollCreationMessage().GetSelectableOptionsCount(); n != 0 {
		t.Errorf("multiple: selectable = %d, want 0 (any number)", n)
	}
}

func TestSendPollValidation(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	thirteen := strings.Split("a b c d e f g h i j k l m", " ")
	for name, tc := range map[string]struct {
		question string
		options  []string
	}{
		"no question":      {"  ", []string{"A", "B"}},
		"one option":       {"Q", []string{"A"}},
		"too many options": {"Q", thirteen},
		"empty option":     {"Q", []string{"A", " "}},
		"duplicate option": {"Q", []string{"A", " A"}},
	} {
		if _, err := c.SendPoll(context.Background(), bobPN.String(), tc.question, tc.options, false); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if len(fake.sent) != 0 {
		t.Errorf("sent = %v, want nothing", fake.sent)
	}
}
