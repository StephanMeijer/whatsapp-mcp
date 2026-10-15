// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"testing"
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestCallChangeFor(t *testing.T) {
	at := time.Unix(1000, 0)
	lid := types.NewJID("111", types.HiddenUserServer)
	pn := types.NewJID("31600000001", types.DefaultUserServer)
	group := types.NewJID("120363000000000000", types.GroupServer)
	canon := func(j types.JID) types.JID {
		if j.ToNonAD() == lid {
			return pn
		}
		return j.ToNonAD()
	}
	meta := types.BasicCallMeta{CallID: "C1", From: types.NewADJID("111", 0, 1), CallCreator: lid, Timestamp: at}
	videoOffer := &waBinary.Node{Tag: "offer", Content: []waBinary.Node{{Tag: "audio"}, {Tag: "video"}}}
	audioOffer := &waBinary.Node{Tag: "offer", Content: []waBinary.Node{{Tag: "audio"}}}

	// 1:1 audio offer from a LID caller: canonical caller, wire creator kept.
	ch, ok := callChangeFor(&events.CallOffer{BasicCallMeta: meta, Data: audioOffer}, canon)
	want := store.Call{CallID: "C1", CallerJID: pn.String(), CreatorWire: lid.String(), ChatJID: pn.String(), StartedAt: 1000}
	if !ok || ch.offer == nil || *ch.offer != want {
		t.Errorf("audio offer = %+v, want %+v", ch.offer, want)
	}
	if ch, _ := callChangeFor(&events.CallOffer{BasicCallMeta: meta, Data: videoOffer}, canon); !ch.offer.Video {
		t.Error("offer with a video child should be a video call")
	}

	// Group video call announced by a notice.
	gm := meta
	gm.GroupJID = group
	ch, _ = callChangeFor(&events.CallOfferNotice{BasicCallMeta: gm, Media: "video", Type: "group"}, canon)
	if !ch.offer.Video || !ch.offer.Group || ch.offer.ChatJID != group.String() || ch.offer.CallerJID != pn.String() {
		t.Errorf("group notice = %+v", ch.offer)
	}

	for _, tt := range []struct {
		evt  any
		want store.CallEvent
	}{
		{&events.CallAccept{BasicCallMeta: meta}, store.CallEventAnswered},
		{&events.CallReject{BasicCallMeta: meta}, store.CallEventRejected},
		{&events.CallTerminate{BasicCallMeta: meta, Reason: "timeout"}, store.CallEventTerminated},
	} {
		ch, ok := callChangeFor(tt.evt, canon)
		if !ok || ch.offer != nil || ch.callID != "C1" || ch.event != tt.want || !ch.at.Equal(at) {
			t.Errorf("%T = %+v, want event %d", tt.evt, ch, tt.want)
		}
	}
	if ch, _ := callChangeFor(&events.CallTerminate{BasicCallMeta: meta, Reason: "timeout"}, canon); ch.reason != "timeout" {
		t.Errorf("terminate reason = %q", ch.reason)
	}
	// Picked up or declined on the phone: only a terminate arrives here, and
	// before the accept or reject (seen live). The reason decides.
	for reason, want := range map[string]store.CallEvent{
		"accepted_elsewhere": store.CallEventAnswered,
		"rejected_elsewhere": store.CallEventRejected,
		"":                   store.CallEventTerminated,
	} {
		if ch, _ := callChangeFor(&events.CallTerminate{BasicCallMeta: meta, Reason: reason}, canon); ch.event != want {
			t.Errorf("terminate with reason %q = event %d, want %d", reason, ch.event, want)
		}
	}
	if _, ok := callChangeFor(&events.CallRelayLatency{BasicCallMeta: meta}, canon); ok {
		t.Error("relay latency is not a call change")
	}
}

func TestCanReject(t *testing.T) {
	if err := canReject(store.Call{CallID: "C", Status: store.CallRinging}); err != nil {
		t.Errorf("ringing call: %v", err)
	}
	for _, s := range []string{store.CallMissed, store.CallAnswered, store.CallRejected, store.CallEnded} {
		if err := canReject(store.Call{CallID: "C", Status: s}); err == nil {
			t.Errorf("%s call: want error", s)
		}
	}
}
