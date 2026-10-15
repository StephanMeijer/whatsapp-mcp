// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"

	"go.mau.fi/whatsmeow/types"

	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

// Options are the guardrails on what the tools may do.
type Options struct {
	// ReadOnly leaves out every write tool, so clients never see them.
	ReadOnly bool
	// AllowRecipients, when non-empty, limits write tools to these JIDs or
	// phone numbers. The account's own number is always allowed.
	AllowRecipients []string
	// Logger records refused writes. Defaults to slog.Default().
	Logger *slog.Logger
	// MediaDir, when set, is the only folder send_media may read files from.
	MediaDir string
}

// policy decides which recipients write tools may target. Every write tool
// must call checkRecipient before acting.
type policy struct {
	readOnly bool
	// allowed holds the parsed allowlist; nil means every recipient is
	// allowed. Entries are canonicalised at check time, so a LID↔phone
	// mapping learned after startup applies immediately.
	allowed []types.JID
	log     *slog.Logger
}

func newPolicy(opts Options) (*policy, error) {
	p := &policy{readOnly: opts.ReadOnly, log: opts.Logger}
	if p.log == nil {
		p.log = slog.Default()
	}
	for _, r := range opts.AllowRecipients {
		jid, err := wa.ParseRecipient(r)
		if err != nil {
			return nil, fmt.Errorf("allowed recipients: %w", err)
		}
		p.allowed = append(p.allowed, jid.ToNonAD())
	}
	return p, nil
}

// checkRecipient parses recipient, canonicalises it (an @lid becomes the
// phone-number JID when known) and reports whether write tools may target
// it. self is the account's own JID ("" when not paired).
func (p *policy) checkRecipient(ctx context.Context, w WhatsApp, recipient, self string) (types.JID, error) {
	parsed, err := parseRecipient(recipient)
	if err != nil {
		return types.JID{}, err
	}
	jid := w.Canonical(ctx, parsed)
	if p.allowed == nil || p.isAllowed(ctx, w, jid) || isSelf(ctx, w, jid, self) {
		return jid, nil
	}
	p.log.Warn("refused write to recipient not in the allowlist", "recipient", jid.String())
	return types.JID{}, fmt.Errorf("recipient %s is not in the allowed recipients list", jid)
}

func (p *policy) isAllowed(ctx context.Context, w WhatsApp, jid types.JID) bool {
	for _, a := range p.allowed {
		if w.Canonical(ctx, a) == jid {
			return true
		}
	}
	return false
}

func isSelf(ctx context.Context, w WhatsApp, jid types.JID, self string) bool {
	if self == "" {
		return false
	}
	own, err := types.ParseJID(self)
	return err == nil && w.Canonical(ctx, own) == jid
}

// parseRecipient is wa.ParseRecipient without the device part.
func parseRecipient(s string) (types.JID, error) {
	jid, err := wa.ParseRecipient(s)
	return jid.ToNonAD(), err
}
