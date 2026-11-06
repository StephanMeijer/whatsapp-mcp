// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
)

func TestPollResults(t *testing.T) {
	const group = "120363000000000001@g.us"
	const alice, bob = "31600000001@s.whatsapp.net", "31600000002@s.whatsapp.net"
	ref, err := proto.Marshal(&waE2E.PollCreationMessage{
		Name:                   proto.String("Lunch?"),
		Options:                []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("Pizza")}, {OptionName: proto.String("Sushi")}},
		SelectableOptionsCount: proto.Uint32(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st := storetest.New(t)
	if err := st.Save(ctx, nil, []store.Message{
		{ChatJID: group, MessageID: "poll", SenderJID: alice, Timestamp: 100, Text: "Lunch?\n- Pizza\n- Sushi", MediaType: store.MediaPoll, MediaRef: ref},
		{ChatJID: group, MessageID: "text", SenderJID: alice, Timestamp: 101, Text: "hi"},
	}); err != nil {
		t.Fatal(err)
	}
	pick := func(o string) string { return hex.EncodeToString(whatsmeow.HashPollOptions([]string{o})[0]) }
	for _, v := range []store.PollVote{
		{ChatJID: group, PollID: "poll", VoterJID: alice, Selected: pick("Sushi"), VotedAt: 110},
		{ChatJID: group, PollID: "poll", VoterJID: bob, Selected: pick("Sushi"), VotedAt: 120},
	} {
		if err := st.RecordPollVote(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	cs := connect(t, &fakeWA{names: map[string]string{alice: "Alice"}}, st, Options{ReadOnly: true})

	var out PollResultsOut
	if res := call(t, cs, "poll_results", map[string]any{"chat_jid": group, "message_id": "poll"}, &out); res.IsError {
		t.Fatalf("poll_results: %v", res.Content)
	}
	want := PollResultsOut{Question: "Lunch?", Options: []PollOptionOut{
		{Name: "Pizza"},
		{Name: "Sushi", Votes: 2, Voters: []PollVoter{{JID: alice, Name: "Alice"}, {JID: bob}}},
	}}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("poll_results =\n %+v\nwant\n %+v", out, want)
	}

	for args, wantErr := range map[[2]string]string{
		{group, "text"}: "not a poll",
		{group, "nope"}: "not found",
		{group, ""}:     "required",
	} {
		res := call(t, cs, "poll_results", map[string]any{"chat_jid": args[0], "message_id": args[1]}, nil)
		if got := res.Content[0].(*mcp.TextContent).Text; !res.IsError || !strings.Contains(got, wantErr) {
			t.Errorf("poll_results(%v) = %q, want an error containing %q", args, got, wantErr)
		}
	}
}
