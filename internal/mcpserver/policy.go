// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
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
}

// policy decides which recipients write tools may target. Every write tool
// must call checkRecipient before acting.
type policy struct {
	readOnly bool
	// allowed holds normalised JIDs; nil means every recipient is allowed.
	allowed map[string]bool
	log     *slog.Logger
}

func newPolicy(opts Options) (*policy, error) {
	p := &policy{readOnly: opts.ReadOnly, log: opts.Logger}
	if p.log == nil {
		p.log = slog.Default()
	}
	if len(opts.AllowRecipients) > 0 {
		p.allowed = make(map[string]bool, len(opts.AllowRecipients))
		for _, r := range opts.AllowRecipients {
			jid, err := wa.ParseRecipient(r)
			if err != nil {
				return nil, fmt.Errorf("allowed recipients: %w", err)
			}
			p.allowed[jid.ToNonAD().String()] = true
		}
	}
	return p, nil
}

// checkRecipient parses recipient and reports whether write tools may target
// it. self is the account's own JID ("" when not paired).
func (p *policy) checkRecipient(recipient, self string) (types.JID, error) {
	jid, err := wa.ParseRecipient(recipient)
	if err != nil {
		return types.JID{}, err
	}
	jid = jid.ToNonAD()
	if p.allowed == nil || p.allowed[jid.String()] || isSelf(jid, self) {
		return jid, nil
	}
	p.log.Warn("refused write to recipient not in the allowlist", "recipient", jid.String())
	return types.JID{}, fmt.Errorf("recipient %s is not in the allowed recipients list "+
		"(note: a contact's @lid JID counts as a different recipient than their phone number)", jid)
}

func isSelf(jid types.JID, self string) bool {
	if self == "" {
		return false
	}
	own, err := types.ParseJID(self)
	return err == nil && own.Server == jid.Server && own.User == jid.User
}
