// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"context"
	"errors"
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
	// GroupAdmin offers the tools that create, change and leave groups.
	GroupAdmin bool
	// InviteLinks lets group_invite_link work while AllowRecipients is set.
	InviteLinks bool
}

// policy decides which recipients write tools may target. Every write tool
// must call checkRecipient before acting.
type policy struct {
	readOnly bool
	// allowed holds the parsed allowlist; nil means every recipient is
	// allowed. Entries are canonicalised at check time, so a LID↔phone
	// mapping learned after startup applies immediately.
	allowed     []types.JID
	log         *slog.Logger
	groupAdmin  bool
	inviteLinks bool
}

func newPolicy(opts Options) (*policy, error) {
	p := &policy{readOnly: opts.ReadOnly, log: opts.Logger, groupAdmin: opts.GroupAdmin, inviteLinks: opts.InviteLinks}
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
// it. The account's own JID is always allowed once paired.
func (p *policy) checkRecipient(ctx context.Context, w WhatsApp, recipient string) (types.JID, error) {
	parsed, err := wa.ParseRecipient(recipient)
	if err != nil {
		return types.JID{}, err
	}
	jid := w.Canonical(ctx, parsed)
	// Status().JID is already the canonical phone-number JID, "" before pairing.
	self := w.Status().JID
	if p.allowed == nil || p.isAllowed(ctx, w, jid) || (self != "" && jid.String() == self) {
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

// maxGroupMembers bounds the people one group call acts on: adding many at
// once looks like spam to WhatsApp.
const maxGroupMembers = 20

// groupChat resolves the group a group tool acts on. Like any write target
// it must pass the allowlist, and it must be a group.
func (p *policy) groupChat(ctx context.Context, w WhatsApp, chat string) (types.JID, error) {
	if chat == "" {
		return types.JID{}, errors.New("chat_jid is required")
	}
	jid, err := p.checkRecipient(ctx, w, chat)
	if err != nil {
		return types.JID{}, err
	}
	if jid.Server != types.GroupServer {
		return types.JID{}, fmt.Errorf("%s is not a group", jid)
	}
	return jid, nil
}

// groupMembers parses the people a group tool acts on into canonical user
// JIDs, without duplicates and never the account itself. With
// checkAllowlist, each must pass the allowlist, and one that doesn't refuses
// the whole call before anything is sent.
func (p *policy) groupMembers(ctx context.Context, w WhatsApp, members []string, checkAllowlist bool) ([]types.JID, error) {
	self := w.Status().JID
	seen := make(map[types.JID]bool, len(members))
	var out []types.JID
	for _, m := range members {
		var jid types.JID
		if checkAllowlist {
			var err error
			if jid, err = p.checkRecipient(ctx, w, m); err != nil {
				return nil, err
			}
		} else {
			parsed, err := wa.ParseRecipient(m)
			if err != nil {
				return nil, err
			}
			jid = w.Canonical(ctx, parsed)
		}
		if jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer {
			return nil, fmt.Errorf("%s is not a person", jid)
		}
		if self != "" && jid.String() == self {
			return nil, errors.New("leave yourself out of the members: you are always in your own groups")
		}
		if !seen[jid] {
			seen[jid] = true
			out = append(out, jid)
		}
	}
	switch {
	case len(out) == 0:
		return nil, errors.New("at least one member is required")
	case len(out) > maxGroupMembers:
		return nil, fmt.Errorf("at most %d members per call, not %d", maxGroupMembers, len(out))
	}
	return out, nil
}

// hasAllowlist reports whether write tools are limited to some recipients.
func (p *policy) hasAllowlist() bool { return p.allowed != nil }
