// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"slices"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func lunchPoll(selectable uint32) *waE2E.Message {
	return &waE2E.Message{PollCreationMessage: &waE2E.PollCreationMessage{
		Name: proto.String("Lunch?"),
		Options: []*waE2E.PollCreationMessage_Option{
			{OptionName: proto.String("Pizza")}, {OptionName: proto.String("Sushi")},
		},
		SelectableOptionsCount: proto.Uint32(selectable),
	}}
}

func voteFor(options ...string) *waE2E.PollVoteMessage {
	return &waE2E.PollVoteMessage{SelectedOptions: whatsmeow.HashPollOptions(options)}
}

func pollVotes(t *testing.T, c *Client, chat, poll string) map[string]string {
	t.Helper()
	votes, err := c.store.PollVotes(context.Background(), chat, poll)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, v := range votes {
		out[v.VoterJID] = v.Selected
	}
	return out
}

func TestOnMessageRecordsPollVotes(t *testing.T) {
	c, fake, _, _ := newTestClient(t)
	update := &waE2E.Message{PollUpdateMessage: &waE2E.PollUpdateMessage{
		PollCreationMessageKey: &waCommon.MessageKey{ID: proto.String("POLL1")},
	}}
	fake.votes = map[string]*waE2E.PollVoteMessage{"V1": voteFor("Sushi")}

	// From Alice's LID in the group: recorded under her number.
	c.onMessage(liveMessage(testGroup, aliceLID, "V1", false, "", update))
	// A vote whatsmeow can't decrypt (a poll from before pairing) is dropped.
	c.onMessage(liveMessage(testGroup, bobPN, "V2", false, "", update))

	got := pollVotes(t, c, testGroup.String(), "POLL1")
	want := selectedHashes(whatsmeow.HashPollOptions([]string{"Sushi"}))
	if len(got) != 1 || got[alicePN.String()] != want {
		t.Errorf("votes = %v, want only Alice's, for Sushi", got)
	}
	if _, err := c.store.MessageByID(context.Background(), testGroup.String(), "V1"); err == nil {
		t.Error("the vote was archived as a message")
	}
}

func TestOnHistorySyncRecordsPollVotes(t *testing.T) {
	c, _, _, _ := newTestClient(t)
	poll := webMsg("POLL1", true, 100, "", lunchPoll(1)).GetMessage()
	poll.PollUpdates = []*waWeb.PollUpdate{
		{PollUpdateMessageKey: &waCommon.MessageKey{Participant: proto.String(aliceLID.String())},
			Vote: voteFor("Pizza"), SenderTimestampMS: proto.Int64(200_000)},
		{PollUpdateMessageKey: &waCommon.MessageKey{FromMe: proto.Bool(true)},
			Vote: voteFor("Pizza", "Sushi"), SenderTimestampMS: proto.Int64(300_000)},
	}
	c.onHistorySync(historySync(waHistorySync.HistorySync_INITIAL_BOOTSTRAP,
		&waHistorySync.Conversation{ID: proto.String(testGroup.String()),
			Messages: []*waHistorySync.HistorySyncMsg{{Message: poll}}}))

	got := pollVotes(t, c, testGroup.String(), "POLL1")
	if got[alicePN.String()] != selectedHashes(whatsmeow.HashPollOptions([]string{"Pizza"})) ||
		got[ownJID.ToNonAD().String()] != selectedHashes(whatsmeow.HashPollOptions([]string{"Pizza", "Sushi"})) || len(got) != 2 {
		t.Errorf("votes = %v, want Alice's and our own", got)
	}
}

func TestTallyPoll(t *testing.T) {
	poll := store.Message{MessageID: "POLL1", MediaType: store.MediaPoll, MediaRef: mediaRef(lunchPoll(1), false)}
	hashes := func(o ...string) string { return selectedHashes(whatsmeow.HashPollOptions(o)) }
	res, err := TallyPoll(poll, []store.PollVote{
		{VoterJID: "a", Selected: hashes("Pizza")},
		{VoterJID: "b", Selected: hashes("Sushi")},
		{VoterJID: "c", Selected: hashes("Pizza")},
		{VoterJID: "d", Selected: ""},                 // withdrew
		{VoterJID: "e", Selected: hashes("Pancakes")}, // an option added later
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Question != "Lunch?" || res.Multiple || len(res.Options) != 2 || res.UnknownVotes != 1 {
		t.Fatalf("result = %+v", res)
	}
	if !slices.Equal(res.Options[0].Voters, []string{"a", "c"}) || !slices.Equal(res.Options[1].Voters, []string{"b"}) {
		t.Errorf("voters = %+v", res.Options)
	}

	multi := store.Message{MediaType: store.MediaPoll, MediaRef: mediaRef(lunchPoll(0), false)}
	if res, _ := TallyPoll(multi, nil); !res.Multiple {
		t.Error("a poll allowing any number of answers is not reported as multiple")
	}
	if _, err := TallyPoll(store.Message{MediaType: store.MediaPoll}, nil); err == nil {
		t.Error("a poll without its options was tallied")
	}
	if _, err := TallyPoll(store.Message{Text: "hi"}, nil); err == nil {
		t.Error("a text message was tallied as a poll")
	}
}
