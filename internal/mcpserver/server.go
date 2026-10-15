// SPDX-License-Identifier: MPL-2.0

// Package mcpserver exposes the archive and the WhatsApp client as MCP tools.
package mcpserver

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

// WhatsApp is the part of *wa.Client the tools use.
type WhatsApp interface {
	Status() wa.Status
	Contacts(ctx context.Context, query string) ([]wa.Contact, error)
	SendText(ctx context.Context, recipient, text string) (store.Message, error)
}

// New returns an MCP server with all tools registered.
func New(version string, whatsapp WhatsApp, st *store.Store) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "whatsapp-mcp", Version: version}, &mcp.ServerOptions{
		Instructions: "Read, search and send WhatsApp messages for the paired account. " +
			"Only messages received while this server ran (plus the initial history sync) are available. " +
			"Use list_chats or search_contacts to find a JID before calling list_messages or send_message.",
	})
	registerTools(s, &tools{wa: whatsapp, store: st})
	return s
}

// Handler serves s over streamable HTTP.
func Handler(s *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
}
