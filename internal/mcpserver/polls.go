// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

type PollResultsIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"the chat's JID"`
	MessageID string `json:"message_id" jsonschema:"id of a message with media_type poll"`
}

type PollVoter struct {
	JID  string `json:"jid"`
	Name string `json:"name,omitempty"`
}

type PollOptionOut struct {
	Name   string      `json:"name"`
	Votes  int         `json:"votes"`
	Voters []PollVoter `json:"voters,omitempty"`
}

type PollResultsOut struct {
	Question string          `json:"question"`
	Multiple bool            `json:"multiple_answers" jsonschema:"true if people may choose more than one option"`
	Options  []PollOptionOut `json:"options"`
	// UnknownOptionVotes counts choices of options this poll doesn't list.
	UnknownOptionVotes int `json:"unknown_option_votes,omitempty" jsonschema:"choices of options not in the archived poll, such as ones added later"`
}

func (t *tools) pollResults(ctx context.Context, _ *mcp.CallToolRequest, in PollResultsIn) (*mcp.CallToolResult, PollResultsOut, error) {
	if in.ChatJID == "" || in.MessageID == "" {
		return nil, PollResultsOut{}, errors.New("chat_jid and message_id are required")
	}
	chat := t.canonicalChat(ctx, in.ChatJID)
	m, err := t.store.MessageByID(ctx, chat, in.MessageID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, PollResultsOut{}, fmt.Errorf("message %s not found in chat %s", in.MessageID, chat)
	}
	if err != nil {
		return nil, PollResultsOut{}, err
	}
	votes, err := t.store.PollVotes(ctx, chat, in.MessageID)
	if err != nil {
		return nil, PollResultsOut{}, err
	}
	res, err := wa.TallyPoll(m, votes)
	if err != nil {
		return nil, PollResultsOut{}, err
	}
	var voters []string
	for _, o := range res.Options {
		voters = append(voters, o.Voters...)
	}
	names := t.senderNames(ctx, voters)
	out := PollResultsOut{Question: res.Question, Multiple: res.Multiple, UnknownOptionVotes: res.UnknownVotes,
		Options: make([]PollOptionOut, len(res.Options))}
	for i, o := range res.Options {
		out.Options[i] = PollOptionOut{Name: o.Name, Votes: len(o.Voters)}
		for _, v := range o.Voters {
			out.Options[i].Voters = append(out.Options[i].Voters, PollVoter{JID: v, Name: names[v]})
		}
	}
	return nil, out, nil
}
