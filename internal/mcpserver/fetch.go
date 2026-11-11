// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	fetchMaxCount    = 50
	fetchMaxWait     = 60
	fetchDefaultWait = 30
)

type FetchOlderIn struct {
	ChatJID     string `json:"chat_jid" jsonschema:"the chat's JID or an international phone number such as +31612345678"`
	Count       int    `json:"count,omitempty" jsonschema:"messages to request (default 50, max 50)"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"how long to wait for the phone (default 30, max 60)"`
}

type FetchOlderOut struct {
	Status          string `json:"status" jsonschema:"fetched, no_older_messages or pending"`
	Fetched         int64  `json:"fetched" jsonschema:"older messages now archived"`
	OldestTimestamp string `json:"oldest_timestamp" jsonschema:"RFC 3339; the oldest archived message in the chat"`
}

func (t *tools) fetchOlderMessages(ctx context.Context, _ *mcp.CallToolRequest, in FetchOlderIn) (*mcp.CallToolResult, FetchOlderOut, error) {
	if in.ChatJID == "" {
		return nil, FetchOlderOut{}, errors.New("chat_jid is required")
	}
	count := in.Count
	if count == 0 {
		count = fetchMaxCount
	}
	if count < 1 || count > fetchMaxCount {
		return nil, FetchOlderOut{}, fmt.Errorf("count must be between 1 and %d", fetchMaxCount)
	}
	wait := in.WaitSeconds
	if wait == 0 {
		wait = fetchDefaultWait
	}
	if wait < 1 || wait > fetchMaxWait {
		return nil, FetchOlderOut{}, fmt.Errorf("wait_seconds must be between 1 and %d", fetchMaxWait)
	}

	chat, err := t.canonicalChat(ctx, in.ChatJID)
	if err != nil {
		return nil, FetchOlderOut{}, fmt.Errorf("chat_jid: %w", err)
	}
	res, err := t.wa.FetchOlder(ctx, chat, count, time.Duration(wait)*time.Second)
	if err != nil {
		return nil, FetchOlderOut{}, err
	}
	return nil, FetchOlderOut{
		Status:          res.Status,
		Fetched:         res.Fetched,
		OldestTimestamp: formatUnix(res.Oldest.Unix()),
	}, nil
}
