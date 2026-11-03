// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// callsDefaultLimit is the default number of calls list_calls returns.
const callsDefaultLimit = 20

type CallOut struct {
	CallID     string `json:"call_id"`
	Caller     string `json:"caller"`
	CallerName string `json:"caller_name,omitempty"`
	Chat       string `json:"chat,omitempty" jsonschema:"the group, for group calls"`
	ChatName   string `json:"chat_name,omitempty"`
	Video      bool   `json:"video"`
	Group      bool   `json:"group"`
	StartedAt  string `json:"started_at" jsonschema:"RFC 3339"`
	EndedAt    string `json:"ended_at,omitempty" jsonschema:"RFC 3339"`
	Status     string `json:"status" jsonschema:"ringing, answered, rejected or missed"`
}

type ListCallsIn struct {
	Since string `json:"since,omitempty" jsonschema:"RFC 3339; only calls that started at or after this time"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum results (default 20, max 500)"`
}

type ListCallsOut struct {
	Calls []CallOut `json:"calls"`
}

func (t *tools) listCalls(ctx context.Context, _ *mcp.CallToolRequest, in ListCallsIn) (*mcp.CallToolResult, ListCallsOut, error) {
	var since time.Time
	if in.Since != "" {
		var err error
		if since, err = time.Parse(time.RFC3339, in.Since); err != nil {
			return nil, ListCallsOut{}, fmt.Errorf("since: want RFC 3339: %w", err)
		}
	}
	limit := in.Limit
	if limit <= 0 {
		limit = callsDefaultLimit
	}
	calls, err := t.store.ListCalls(ctx, since, limit)
	if err != nil {
		return nil, ListCallsOut{}, err
	}
	return nil, ListCallsOut{Calls: t.toCalls(ctx, calls)}, nil
}

// toCalls names callers with one contacts lookup and groups from the archive.
func (t *tools) toCalls(ctx context.Context, calls []store.Call) []CallOut {
	callers := make([]string, len(calls))
	var groups []string
	for i, c := range calls {
		callers[i] = c.CallerJID
		if c.Group {
			groups = append(groups, c.ChatJID)
		}
	}
	names := t.senderNames(ctx, callers)
	chats, err := t.store.ChatsByJID(ctx, groups)
	if err != nil {
		chats = nil // names are optional; the calls are not
	}
	out := make([]CallOut, len(calls))
	for i, c := range calls {
		out[i] = CallOut{
			CallID: c.CallID, Caller: c.CallerJID, CallerName: names[c.CallerJID],
			Video: c.Video, Group: c.Group, StartedAt: formatUnix(c.StartedAt), Status: c.Status,
		}
		if c.Group {
			out[i].Chat, out[i].ChatName = c.ChatJID, chats[c.ChatJID].Name
		}
		if c.EndedAt > 0 {
			out[i].EndedAt = formatUnix(c.EndedAt)
		}
	}
	return out
}

type RejectCallIn struct {
	CallID string `json:"call_id" jsonschema:"from list_calls"`
}

func (t *tools) rejectCall(ctx context.Context, _ *mcp.CallToolRequest, in RejectCallIn) (*mcp.CallToolResult, CallOut, error) {
	if in.CallID == "" {
		return nil, CallOut{}, errors.New("call_id is required")
	}
	call, err := t.store.CallByID(ctx, in.CallID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, CallOut{}, fmt.Errorf("unknown call %s; see list_calls", in.CallID)
	}
	if err != nil {
		return nil, CallOut{}, err
	}
	if _, err := t.policy.checkRecipient(ctx, t.wa, call.CallerJID, t.wa.Status().JID); err != nil {
		return nil, CallOut{}, err
	}
	rejected, err := t.wa.RejectCall(ctx, in.CallID)
	if err != nil {
		return nil, CallOut{}, err
	}
	return nil, t.toCalls(ctx, []store.Call{rejected})[0], nil
}
