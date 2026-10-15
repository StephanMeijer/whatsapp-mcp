// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// syncGroupNames names archived group chats after the groups the account is
// in, with one request for all of them. It catches renames made while the
// server was offline and groups the history sync left unnamed. Groups the
// account has left are not returned, so they keep the name they had.
func (c *Client) syncGroupNames(ctx context.Context) {
	groups, err := c.cli.GetJoinedGroups(ctx)
	if err != nil {
		c.log.Warn("sync group names: list joined groups", "err", err)
		return
	}
	names := groupNames(groups)
	c.nameAfterMembers(ctx, groups, names)
	changed, err := c.store.RenameChats(ctx, names)
	if err != nil {
		c.log.Error("sync group names", "err", err)
		return
	}
	c.log.Info("synced group names", "joined", len(groups), "with_name", len(names), "renamed", changed)
}

// onGroupInfo applies a live group rename.
func (c *Client) onGroupInfo(e *events.GroupInfo) {
	jid, name, ok := renameFromGroupInfo(e)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
	defer cancel()
	if _, err := c.store.RenameChats(ctx, map[string]string{jid: name}); err != nil {
		c.log.Error("rename group", "err", err, "group", jid)
	}
}

// onJoinedGroup records a group the account was just added to, so it has its
// name before its first message arrives.
func (c *Client) onJoinedGroup(e *events.JoinedGroup) {
	if e.Name == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
	defer cancel()
	chat := store.Chat{JID: e.JID.ToNonAD().String(), Name: e.Name}
	if err := c.store.Save(ctx, []store.Chat{chat}, nil); err != nil {
		c.log.Error("record joined group", "err", err, "group", chat.JID)
	}
}

// nameGroupIfNeeded looks up the name of a group that sent a message but has
// no name in the archive, at most once per group per run. It runs off the
// event loop, since it may make a network request.
func (c *Client) nameGroupIfNeeded(group types.JID) {
	jid := group.ToNonAD().String()
	if !c.groupLookups.shouldLookup(jid) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
		defer cancel()
		chats, err := c.store.ChatsByJID(ctx, []string{jid})
		if err != nil || chats[jid].Name != "" {
			return
		}
		info, err := c.cli.GetGroupInfo(ctx, group.ToNonAD())
		if err != nil {
			c.log.Debug("look up group name", "err", err, "group", jid)
			return
		}
		infos := []*types.GroupInfo{info}
		// A nameless community subgroup is named after its community.
		if info.Name == "" && !info.LinkedParentJID.IsEmpty() {
			if parent, err := c.cli.GetGroupInfo(ctx, info.LinkedParentJID); err == nil {
				infos = append(infos, parent)
			}
		}
		names := groupNames(infos)
		c.nameAfterMembers(ctx, infos[:1], names)
		if names[jid] == "" {
			return
		}
		if _, err := c.store.RenameChats(ctx, map[string]string{jid: names[jid]}); err != nil {
			c.log.Error("name group", "err", err, "group", jid)
		}
	}()
}

// groupNames maps group JIDs to their names, skipping groups without one.
// A community's subgroups often have no name of their own; they take the
// community's name, marked as its announcements group where that applies.
func groupNames(groups []*types.GroupInfo) map[string]string {
	own := make(map[string]string, len(groups))
	for _, g := range groups {
		if g != nil && g.Name != "" {
			own[g.JID.ToNonAD().String()] = g.Name
		}
	}
	out := make(map[string]string, len(groups))
	for _, g := range groups {
		if g == nil {
			continue
		}
		jid := g.JID.ToNonAD().String()
		name := own[jid]
		if name == "" && !g.LinkedParentJID.IsEmpty() {
			if community := own[g.LinkedParentJID.ToNonAD().String()]; community != "" {
				name = community
				if g.IsDefaultSubGroup {
					name += " (announcements)"
				}
			}
		}
		if name != "" {
			out[jid] = name
		}
	}
	return out
}

// nameAfterMembers adds names for groups that have none of their own (small
// groups can be created without one) by listing their other members, as
// WhatsApp displays them. Member names come from one contacts lookup.
func (c *Client) nameAfterMembers(ctx context.Context, groups []*types.GroupInfo, names map[string]string) {
	var nameless []*types.GroupInfo
	var members []types.JID
	for _, g := range groups {
		if g == nil || names[g.JID.ToNonAD().String()] != "" {
			continue
		}
		nameless = append(nameless, g)
		for _, p := range g.Participants {
			members = append(members, c.Canonical(ctx, participantJID(p)))
		}
	}
	if len(nameless) == 0 {
		return
	}
	known, err := c.Names(ctx, members)
	if err != nil {
		c.log.Warn("name groups after members: look up names", "err", err)
		return
	}
	self := map[types.JID]bool{}
	if id := c.cli.Store.ID; id != nil {
		self[id.ToNonAD()] = true
	}
	if lid := c.cli.Store.GetLID(); !lid.IsEmpty() {
		self[lid.ToNonAD()] = true
	}
	nameOf := func(p types.GroupParticipant) (string, bool) {
		jid := c.Canonical(ctx, participantJID(p))
		if self[jid] || self[p.JID.ToNonAD()] || self[p.LID.ToNonAD()] {
			return "", false
		}
		if n := known[jid]; n != "" {
			return n, true
		}
		if jid.Server == types.DefaultUserServer {
			return "+" + jid.User, true
		}
		return "", true // a member we can't name
	}
	for _, g := range nameless {
		if n := memberGroupName(g.Participants, nameOf); n != "" {
			names[g.JID.ToNonAD().String()] = n
		}
	}
}

// participantJID prefers a member's phone-number JID over their LID.
func participantJID(p types.GroupParticipant) types.JID {
	if !p.PhoneNumber.IsEmpty() {
		return p.PhoneNumber.ToNonAD()
	}
	return p.JID.ToNonAD()
}

// maxMemberNames caps how many members a member-derived group name lists.
const maxMemberNames = 5

// memberGroupName lists members' names, sorted, like "Alice, Bob" or
// "Alice, Bob, Carol, Dave, Erin +2". nameOf reports false for members to
// leave out (the account itself) and "" for members it cannot name, who are
// counted in the "+N" but not listed. It returns "" if nobody can be named.
func memberGroupName(participants []types.GroupParticipant, nameOf func(types.GroupParticipant) (string, bool)) string {
	var named []string
	unnamed := 0
	for _, p := range participants {
		n, include := nameOf(p)
		switch {
		case !include:
		case n == "":
			unnamed++
		default:
			named = append(named, n)
		}
	}
	if len(named) == 0 {
		return ""
	}
	sort.Strings(named)
	extra := unnamed
	if len(named) > maxMemberNames {
		extra += len(named) - maxMemberNames
		named = named[:maxMemberNames]
	}
	out := strings.Join(named, ", ")
	if extra > 0 {
		out += fmt.Sprintf(" +%d", extra)
	}
	return out
}

// renameFromGroupInfo extracts a rename from a group change event.
func renameFromGroupInfo(e *events.GroupInfo) (jid, name string, ok bool) {
	if e == nil || e.Name == nil || e.Name.Name == "" {
		return "", "", false
	}
	return e.JID.ToNonAD().String(), e.Name.Name, true
}

// groupLookups remembers which groups have had their name looked up.
type groupLookups struct {
	mu   sync.Mutex
	done map[string]bool
}

// shouldLookup reports true the first time it sees jid, false afterwards.
func (g *groupLookups) shouldLookup(jid string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.done[jid] {
		return false
	}
	if g.done == nil {
		g.done = map[string]bool{}
	}
	g.done[jid] = true
	return true
}
