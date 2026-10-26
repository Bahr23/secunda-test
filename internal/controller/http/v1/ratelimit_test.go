package v1

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimiterRefillIsolationAndBoundedStorage(t *testing.T) {
	now := time.Unix(100, 0)
	l := newLimiter(2, 2, 2, time.Minute, func() time.Time { return now })
	for _, want := range []time.Duration{0, 0, 500 * time.Millisecond} {
		if l.allow("a") != want {
			t.Fatal("burst budget is incorrect")
		}
	}
	if l.allow("b") != 0 {
		t.Fatal("identities share a budget")
	}
	if l.allow("c") == 0 || len(l.buckets) != 2 {
		t.Fatal("capacity overflow evicted an active identity")
	}
	now = now.Add(250 * time.Millisecond)
	if l.allow("a") != 250*time.Millisecond {
		t.Fatal("rejected requests reset or consume refill progress")
	}
	now = now.Add(250 * time.Millisecond)
	if l.allow("a") != 0 || l.allow("a") == 0 {
		t.Fatal("one refill token must allow exactly one request")
	}
	now = now.Add(2 * time.Minute)
	if l.allow("c") != 0 || len(l.buckets) != 1 {
		t.Fatal("idle entries were not reclaimed")
	}
}

func TestLimiterConcurrentRequestsCannotOverspend(t *testing.T) {
	now := time.Now()
	l := newLimiter(1, 10, 100, time.Minute, func() time.Time { return now })
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if l.allow("same-user") == 0 {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 10 {
		t.Fatalf("accepted %d requests from a budget of 10", accepted.Load())
	}
}
