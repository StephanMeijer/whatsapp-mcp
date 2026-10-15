// SPDX-License-Identifier: MPL-2.0

// Package mcpserver exposes the archive and the WhatsApp client as MCP tools.
package mcpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

// WhatsApp is the part of *wa.Client the tools use.
type WhatsApp interface {
	Status() wa.Status
	Contacts(ctx context.Context, query string) ([]wa.Contact, error)
	SendText(ctx context.Context, recipient, text string) (store.Message, error)
	// Canonical maps a JID to the form the archive uses: the phone-number
	// JID for an @lid whose number is known, without a device part.
	Canonical(ctx context.Context, jid types.JID) types.JID
	// Names returns display names for the JIDs it knows; others are absent.
	Names(ctx context.Context, jids []types.JID) (map[types.JID]string, error)
	// FetchOlder asks the phone for messages older than the archive has.
	FetchOlder(ctx context.Context, chatJID string, count int, wait time.Duration) (wa.FetchResult, error)
}

// New returns an MCP server with the tools opts allows. It fails if an
// allowed recipient is not a valid JID or phone number.
func New(version string, whatsapp WhatsApp, st *store.Store, opts Options) (*mcp.Server, error) {
	pol, err := newPolicy(opts)
	if err != nil {
		return nil, err
	}
	instructions := "Read, search and send WhatsApp messages for the paired account. " +
		"Only messages received while this server ran (plus the initial history sync) are available. " +
		"Use list_chats or search_contacts to find a JID before calling list_messages or send_message."
	switch {
	case pol.readOnly:
		instructions = "Read and search WhatsApp messages for the paired account. This server is read-only: " +
			"it cannot send messages. Only messages received while this server ran (plus the initial history sync) " +
			"are available. Use list_chats or search_contacts to find a JID before calling list_messages."
	case pol.allowed != nil:
		instructions += " Sending is limited to an allowlist of recipients; other recipients are refused."
	}

	s := mcp.NewServer(&mcp.Implementation{Name: "whatsapp-mcp", Version: version}, &mcp.ServerOptions{
		Instructions: instructions,
	})
	registerTools(s, &tools{wa: whatsapp, store: st, policy: pol})
	return s, nil
}

// Handler serves s over streamable HTTP.
func Handler(s *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
}
