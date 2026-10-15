// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"sync"
	"testing"
	"time"
)

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestPendingFetches(t *testing.T) {
	var p pendingFetches

	a, releaseA, err := p.register("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.register("a"); err == nil {
		t.Error("second register for the same chat: want error")
	}
	b, releaseB, err := p.register("b")
	if err != nil {
		t.Fatal("other chats are independent:", err)
	}

	p.resolve("nobody") // no waiter: no effect, no panic
	p.resolve("a")
	if !isClosed(a) || isClosed(b) {
		t.Fatalf("resolve(a): a closed=%v b closed=%v, want true false", isClosed(a), isClosed(b))
	}
	p.resolve("a") // already resolved: must not close twice
	releaseA()

	p.setSession("b", "REQ1")
	p.resolveSession("unknown")
	if isClosed(b) {
		t.Fatal("unknown session resolved b")
	}
	p.resolveSession("REQ1")
	if !isClosed(b) {
		t.Fatal("resolveSession(REQ1) did not wake b")
	}
	releaseB()

	// After release, the chat can be registered again and the old session is gone.
	b2, releaseB2, err := p.register("b")
	if err != nil {
		t.Fatal("register after release:", err)
	}
	p.resolveSession("REQ1")
	if isClosed(b2) {
		t.Error("stale session ID woke a new request")
	}
	releaseB2()
}

func TestPendingFetchesConcurrent(t *testing.T) {
	var p pendingFetches
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			chat := string(rune('a' + i%5))
			ch, release, err := p.register(chat)
			if err != nil {
				return // another goroutine holds this chat
			}
			p.setSession(chat, chat+"-req")
			go p.resolveSession(chat + "-req")
			<-ch
			release()
		}()
	}
	wg.Wait()
}

func TestRateLimit(t *testing.T) {
	r := rateLimit{interval: 2 * time.Second}
	t0 := time.Unix(1000, 0)
	if err := r.allow(t0); err != nil {
		t.Fatal("first request:", err)
	}
	if err := r.allow(t0.Add(time.Second)); err == nil {
		t.Error("request within the interval: want error")
	}
	if err := r.allow(t0.Add(2 * time.Second)); err != nil {
		t.Error("request after the interval:", err)
	}
}
