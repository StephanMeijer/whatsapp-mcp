// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"sort"

	"go.mau.fi/whatsmeow/types"
)

// Canonical returns the JID the archive uses for a user: the phone-number JID
// when the @lid privacy ID's number is known, otherwise jid itself, always
// without a device part. Group and other JIDs are returned unchanged.
//
// whatsmeow learns the LID↔phone mapping from message metadata, history
// sync and lookups and keeps it in its own store; this only reads it.
func (c *Client) Canonical(ctx context.Context, jid types.JID) types.JID {
	jid = jid.ToNonAD()
	if jid.Server != types.HiddenUserServer {
		return jid
	}
	pn, err := c.cli.Store.LIDs.GetPNForLID(ctx, jid)
	if err != nil {
		c.log.Debug("look up phone number for LID", "lid", jid.String(), "err", err)
		return jid
	}
	if pn.IsEmpty() {
		return jid
	}
	return pn.ToNonAD()
}

// memoCanonical wraps Canonical with a cache, for bulk work such as a
// history sync where many messages share a few senders.
func (c *Client) memoCanonical(ctx context.Context) func(types.JID) types.JID {
	seen := map[types.JID]types.JID{}
	return func(jid types.JID) types.JID {
		if out, ok := seen[jid]; ok {
			return out
		}
		out := c.Canonical(ctx, jid)
		seen[jid] = out
		return out
	}
}

// reconcile brings archived data in line with what is now known: chats and
// senders stored under an @lid whose phone number has since been learned are
// merged, then chats are named after contacts. It runs on every connect and
// after every history sync, which is when new mappings and names arrive.
func (c *Client) reconcile(ctx context.Context) {
	c.reconcileMu.Lock()
	defer c.reconcileMu.Unlock()
	c.mergeLIDChats(ctx)
	c.syncChatNames(ctx)
}

func (c *Client) reconcileWithTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), reconcileTimeout)
	defer cancel()
	c.reconcile(ctx)
}

func (c *Client) mergeLIDChats(ctx context.Context) {
	lids, err := c.store.LIDJIDs(ctx)
	if err != nil {
		c.log.Error("merge LID chats: list LIDs", "err", err)
		return
	}
	renames := make(map[string]string, len(lids))
	for _, s := range lids {
		lid, err := types.ParseJID(s)
		if err != nil {
			continue
		}
		if pn := c.Canonical(ctx, lid); pn != lid {
			renames[s] = pn.String()
		}
	}
	if len(renames) == 0 {
		return
	}
	merged, err := c.store.MergeJIDs(ctx, renames)
	if err != nil {
		c.log.Error("merge LID chats", "err", err, "merged_before_error", merged)
		return
	}
	if merged > 0 {
		c.log.Info("merged @lid chats and senders into phone-number JIDs", "jids", merged,
			"still_unmapped", len(lids)-len(renames))
	}
}

// canonicalContacts returns the address book keyed by canonical JID, with an
// @lid entry and the phone-number entry for the same person merged.
func (c *Client) canonicalContacts(ctx context.Context) (map[types.JID]types.ContactInfo, error) {
	all, err := c.cli.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return nil, err
	}
	return mergeContacts(all, c.memoCanonical(ctx)), nil
}

// mergeContacts re-keys contacts by canonical JID. Where two entries land on
// the same JID, fields from the phone-number entry win and empty fields are
// filled from the other.
func mergeContacts(all map[types.JID]types.ContactInfo, canon func(types.JID) types.JID) map[types.JID]types.ContactInfo {
	keys := make([]types.JID, 0, len(all))
	for jid := range all {
		keys = append(keys, jid)
	}
	// Phone-number (and other non-LID) entries first, so they take precedence.
	sort.Slice(keys, func(i, j int) bool {
		li, lj := keys[i].Server == types.HiddenUserServer, keys[j].Server == types.HiddenUserServer
		if li != lj {
			return !li
		}
		return keys[i].String() < keys[j].String()
	})

	out := make(map[types.JID]types.ContactInfo, len(all))
	for _, jid := range keys {
		key := canon(jid)
		info := all[jid]
		existing, ok := out[key]
		if !ok {
			out[key] = info
			continue
		}
		existing.Found = existing.Found || info.Found
		existing.FirstName = firstNonEmpty(existing.FirstName, info.FirstName)
		existing.FullName = firstNonEmpty(existing.FullName, info.FullName)
		existing.PushName = firstNonEmpty(existing.PushName, info.PushName)
		existing.BusinessName = firstNonEmpty(existing.BusinessName, info.BusinessName)
		existing.RedactedPhone = firstNonEmpty(existing.RedactedPhone, info.RedactedPhone)
		out[key] = existing
	}
	return out
}
