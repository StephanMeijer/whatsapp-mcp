// SPDX-License-Identifier: MPL-2.0

package mcpserver

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/StephanMeijer/whatsapp-mcp/internal/storetest"
	"github.com/StephanMeijer/whatsapp-mcp/internal/wa"
)

func TestFetchOlderMessages(t *testing.T) {
	oldest := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, status := range []string{wa.FetchFetched, wa.FetchNoOlder, wa.FetchPending} {
		t.Run(status, func(t *testing.T) {
			fake := &fakeWA{
				lids:  map[string]string{"111@lid": "31600000001@s.whatsapp.net"},
				fetch: wa.FetchResult{Status: status, Fetched: 7, Oldest: oldest},
			}
			cs := connect(t, fake, storetest.New(t), Options{})
			var out FetchOlderOut
			call(t, cs, "fetch_older_messages", map[string]any{"chat_jid": "111@lid"}, &out)
			if out.Status != status || out.Fetched != 7 || out.OldestTimestamp != "2024-01-02T03:04:05Z" {
				t.Errorf("out = %+v", out)
			}
			// Canonical chat, default count 50 and wait 30s.
			if want := []string{"31600000001@s.whatsapp.net count=50 wait=30s"}; !slices.Equal(fake.fetchArgs, want) {
				t.Errorf("FetchOlder calls = %v, want %v", fake.fetchArgs, want)
			}
		})
	}
}

func TestFetchOlderMessagesArgs(t *testing.T) {
	fake := &fakeWA{fetch: wa.FetchResult{Status: wa.FetchFetched}}
	cs := connect(t, fake, storetest.New(t), Options{})

	call(t, cs, "fetch_older_messages", map[string]any{"chat_jid": "1@g.us", "count": 10, "wait_seconds": 5}, nil)
	if want := []string{"1@g.us count=10 wait=5s"}; !slices.Equal(fake.fetchArgs, want) {
		t.Errorf("FetchOlder calls = %v, want %v", fake.fetchArgs, want)
	}

	for _, args := range []map[string]any{
		{},
		{"chat_jid": "1@g.us", "count": -1},
		{"chat_jid": "1@g.us", "count": 51},
		{"chat_jid": "1@g.us", "wait_seconds": -1},
		{"chat_jid": "1@g.us", "wait_seconds": 61},
	} {
		if res := call(t, cs, "fetch_older_messages", args, nil); !res.IsError {
			t.Errorf("fetch_older_messages(%v): want tool error", args)
		}
	}
	if len(fake.fetchArgs) != 1 {
		t.Errorf("invalid input reached FetchOlder: %v", fake.fetchArgs)
	}

	fake.fetchErr = errors.New("a request for older messages in 1@g.us is already pending")
	if res := call(t, cs, "fetch_older_messages", map[string]any{"chat_jid": "1@g.us"}, nil); !res.IsError {
		t.Error("FetchOlder error: want tool error")
	}
}
