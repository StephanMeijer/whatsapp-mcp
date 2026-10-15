// SPDX-License-Identifier: MPL-2.0

package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/StephanMeijer/whatsapp-mcp/internal/store"
)

func TestCallLifecycle(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *store.Store) {
		ctx := context.Background()
		at := time.Unix(1000, 0)
		offer := func(id string, startedAt int64, video bool) {
			t.Helper()
			must(t, s.RecordCallOffer(ctx, store.Call{
				CallID: id, CallerJID: "1@s.whatsapp.net", CreatorWire: "111@lid", ChatJID: "1@s.whatsapp.net",
				Video: video, StartedAt: startedAt,
			}))
		}
		status := func(id string) string {
			t.Helper()
			c, err := s.CallByID(ctx, id)
			must(t, err)
			return c.Status
		}
		update := func(id string, ev store.CallEvent) bool {
			t.Helper()
			changed, err := s.UpdateCall(ctx, id, ev, at, "reason-"+id)
			must(t, err)
			return changed
		}

		// Missed: ringing, then terminated.
		offer("missed", 10, false)
		if status("missed") != store.CallRinging {
			t.Fatal("new call should ring")
		}
		if !update("missed", store.CallEventTerminated) || status("missed") != store.CallMissed {
			t.Errorf("terminate while ringing: %s, want missed", status("missed"))
		}

		// Answered, then ended.
		offer("answered", 20, true)
		update("answered", store.CallEventAnswered)
		update("answered", store.CallEventTerminated)
		if c, _ := s.CallByID(ctx, "answered"); c.Status != store.CallEnded || c.EndedAt != at.Unix() || c.Reason != "reason-answered" {
			t.Errorf("answered call = %+v, want ended", c)
		}

		// Answered on the phone: the terminate (reason accepted_elsewhere,
		// mapped to an answer) arrives before the accept, as seen live.
		offer("elsewhere", 25, true)
		update("elsewhere", store.CallEventAnswered)
		if update("elsewhere", store.CallEventAnswered) || status("elsewhere") != store.CallAnswered {
			t.Errorf("answered elsewhere: %s, want answered", status("elsewhere"))
		}

		// Rejected stays rejected; statuses never move backwards.
		offer("rejected", 30, false)
		update("rejected", store.CallEventRejected)
		if update("rejected", store.CallEventAnswered) || update("rejected", store.CallEventTerminated) || status("rejected") != store.CallRejected {
			t.Errorf("rejected call changed: %s", status("rejected"))
		}
		offer("missed", 10, false) // a late, repeated offer
		if status("missed") != store.CallMissed {
			t.Errorf("repeated offer reset a missed call to %s", status("missed"))
		}

		// A repeated offer adds video, never removes it.
		offer("answered", 20, false)
		if c, _ := s.CallByID(ctx, "answered"); !c.Video {
			t.Error("repeated offer cleared the video flag")
		}

		// Events for calls never offered are ignored.
		if update("unknown", store.CallEventTerminated) {
			t.Error("event for an unknown call reported a change")
		}
		if _, err := s.CallByID(ctx, "unknown"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("CallByID(unknown): %v, want ErrNotFound", err)
		}

		calls, err := s.ListCalls(ctx, time.Time{}, 0)
		must(t, err)
		var got []string
		for _, c := range calls {
			got = append(got, c.CallID)
		}
		if want := []string{"rejected", "elsewhere", "answered", "missed"}; !slices.Equal(got, want) {
			t.Errorf("ListCalls = %v, want newest first %v", got, want)
		}
		calls, err = s.ListCalls(ctx, time.Unix(15, 0), 1)
		must(t, err)
		if len(calls) != 1 || calls[0].CallID != "rejected" {
			t.Errorf("ListCalls since/limit = %+v", calls)
		}
	})
}
