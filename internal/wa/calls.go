// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"errors"
	"fmt"
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

// callChange is what a call event means for the archive: either a new
// ringing call (offer) or an event on a known one.
type callChange struct {
	offer  *store.Call
	callID string
	event  store.CallEvent
	at     time.Time
	reason string
}

// callChangeFor maps whatsmeow's call events. Answering or placing calls is
// not supported, so only incoming calls are tracked.
func callChangeFor(evt any, canon func(types.JID) types.JID) (callChange, bool) {
	switch e := evt.(type) {
	case *events.CallOffer:
		c := offerCall(e.BasicCallMeta, canon)
		c.Video = hasChild(e.Data, "video")
		return callChange{offer: &c}, true
	case *events.CallOfferNotice:
		c := offerCall(e.BasicCallMeta, canon)
		c.Video = e.Media == "video"
		c.Group = c.Group || e.Type == "group"
		return callChange{offer: &c}, true
	case *events.CallAccept:
		return callChange{callID: e.CallID, event: store.CallEventAnswered, at: e.Timestamp}, true
	case *events.CallReject:
		return callChange{callID: e.CallID, event: store.CallEventRejected, at: e.Timestamp}, true
	case *events.CallTerminate:
		// When the call is picked up or declined on another of the account's
		// devices, this device only gets a terminate, and it can arrive before
		// the accept or reject; the reason says which it was.
		ev := store.CallEventTerminated
		switch e.Reason {
		case "accepted_elsewhere":
			ev = store.CallEventAnswered
		case "rejected_elsewhere":
			ev = store.CallEventRejected
		}
		return callChange{callID: e.CallID, event: ev, at: e.Timestamp, reason: e.Reason}, true
	}
	return callChange{}, false
}

func offerCall(m types.BasicCallMeta, canon func(types.JID) types.JID) store.Call {
	creator := m.CallCreator
	if creator.IsEmpty() {
		creator = m.From
	}
	caller := canon(creator)
	c := store.Call{
		CallID:      m.CallID,
		CallerJID:   caller.String(),
		CreatorWire: creator.ToNonAD().String(),
		ChatJID:     caller.String(),
		StartedAt:   m.Timestamp.Unix(),
	}
	if !m.GroupJID.IsEmpty() {
		c.ChatJID = m.GroupJID.ToNonAD().String()
		c.Group = true
	}
	return c
}

func hasChild(n *waBinary.Node, tag string) bool {
	return n != nil && n.GetChildByTag(tag).Tag == tag
}

// onCallEvent records a call event.
func (c *Client) onCallEvent(evt any) {
	ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
	defer cancel()
	ch, ok := callChangeFor(evt, func(j types.JID) types.JID { return c.Canonical(ctx, j) })
	if !ok {
		return
	}
	if ch.offer != nil {
		if err := c.store.RecordCallOffer(ctx, *ch.offer); err != nil {
			c.log.Error("record incoming call", "err", err, "call", ch.offer.CallID)
			return
		}
		c.log.Info("incoming call", "call", ch.offer.CallID, "video", ch.offer.Video, "group", ch.offer.Group)
		return
	}
	changed, err := c.store.UpdateCall(ctx, ch.callID, ch.event, ch.at, ch.reason)
	if err != nil {
		c.log.Error("update call", "err", err, "call", ch.callID)
		return
	}
	c.log.Info("call event", "call", ch.callID, "event", fmt.Sprintf("%T", evt), "reason", ch.reason, "applied", changed)
}

// RejectCall declines a call that is still ringing.
func (c *Client) RejectCall(ctx context.Context, callID string) (store.Call, error) {
	if !c.cli.IsLoggedIn() {
		return store.Call{}, errors.New("not logged in to WhatsApp")
	}
	call, err := c.store.CallByID(ctx, callID)
	if err != nil {
		return store.Call{}, err
	}
	if err := canReject(call); err != nil {
		return store.Call{}, err
	}
	creator, err := types.ParseJID(call.CreatorWire)
	if err != nil {
		return store.Call{}, fmt.Errorf("call %s has no usable caller: %w", callID, err)
	}
	if err := c.cli.RejectCall(ctx, creator, callID); err != nil {
		return store.Call{}, fmt.Errorf("reject call: %w", err)
	}
	if _, err := c.store.UpdateCall(ctx, callID, store.CallEventRejected, time.Now(), "rejected via MCP"); err != nil {
		c.log.Error("record rejected call", "err", err, "call", callID)
	}
	c.log.Info("rejected call", "call", callID)
	call.Status = store.CallRejected
	return call, nil
}

// canReject allows rejecting only a call that is still ringing.
func canReject(c store.Call) error {
	if c.Status != store.CallRinging {
		return fmt.Errorf("call %s is no longer ringing (it was %s)", c.CallID, c.Status)
	}
	return nil
}
