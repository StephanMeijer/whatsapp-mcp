// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

// statusLifetime is how long WhatsApp shows a status update.
const statusLifetime = 24 * time.Hour

type ListStatusUpdatesIn struct {
	Since string `json:"since,omitempty" jsonschema:"RFC 3339; updates at or after this time (default: the last 24 hours, how long WhatsApp shows them)"`
	From  string `json:"from,omitempty" jsonschema:"only this person's updates: a JID or an international phone number"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum updates in total (default 50, max 500)"`
}

type StatusAuthor struct {
	JID     string    `json:"jid"`
	Name    string    `json:"name,omitempty"`
	Updates []Message `json:"updates" jsonschema:"newest first; media can be fetched with get_media"`
}

type ListStatusUpdatesOut struct {
	Authors []StatusAuthor `json:"authors" jsonschema:"whoever posted most recently first"`
}

func (t *tools) listStatusUpdates(ctx context.Context, _ *mcp.CallToolRequest, in ListStatusUpdatesIn) (*mcp.CallToolResult, ListStatusUpdatesOut, error) {
	since := time.Now().Add(-statusLifetime)
	if in.Since != "" {
		var err error
		if since, err = time.Parse(time.RFC3339, in.Since); err != nil {
			return nil, ListStatusUpdatesOut{}, fmt.Errorf("since: want RFC 3339, such as 2026-01-02T15:04:05Z: %w", err)
		}
	}
	var author string
	if in.From != "" {
		jid, err := wa.ParseRecipient(in.From)
		if err != nil {
			return nil, ListStatusUpdatesOut{}, fmt.Errorf("from: %w", err)
		}
		author = t.wa.Canonical(ctx, jid).String()
	}
	msgs, err := t.store.StatusUpdates(ctx, since, author, in.Limit)
	if err != nil {
		return nil, ListStatusUpdatesOut{}, err
	}
	out := ListStatusUpdatesOut{Authors: []StatusAuthor{}}
	index := map[string]int{}
	for _, m := range t.toMessages(ctx, msgs) {
		i, ok := index[m.Sender]
		if !ok {
			i = len(out.Authors)
			index[m.Sender] = i
			out.Authors = append(out.Authors, StatusAuthor{JID: m.Sender, Name: m.SenderName})
		}
		out.Authors[i].Updates = append(out.Authors[i].Updates, m)
	}
	return nil, out, nil
}
