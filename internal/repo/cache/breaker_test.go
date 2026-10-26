package cache

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"
)

func TestCircuitBreakerStopsCallsAndRecoversWithOneProbe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		log := slog.New(slog.NewJSONHandler(&logs, nil))
		c := &Tasks{log: log, timeout: time.Second, breaker: newBreaker(log, Options{FailureThreshold: 2, OpenTimeout: time.Minute})}
		calls := 0
		down := func(context.Context) ([]byte, error) { calls++; return nil, errors.New("dependency offline") }
		for range 3 {
			_, _ = c.execute(t.Context(), "get", down)
		}
		if calls != 2 {
			t.Fatalf("open breaker made %d dependency calls", calls)
		}
		time.Sleep(time.Minute + time.Nanosecond)
		started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			_, _ = c.execute(t.Context(), "get", func(context.Context) ([]byte, error) { close(started); <-release; return nil, redis.Nil })
		}()
		<-started
		if _, err := c.execute(t.Context(), "get", down); !errors.Is(err, gobreaker.ErrTooManyRequests) {
			t.Fatalf("parallel half-open call accepted: %v", err)
		}
		close(release)
		<-done
		_, err := c.execute(t.Context(), "get", func(context.Context) ([]byte, error) { calls++; return []byte("recovered"), nil })
		if err != nil || calls != 3 {
			t.Fatalf("breaker did not close after a healthy cache miss: %v", err)
		}
		for _, state := range []string{`"to":"open"`, `"to":"half-open"`, `"to":"closed"`} {
			if !strings.Contains(logs.String(), state) {
				t.Fatalf("missing transition %s", state)
			}
		}
	})
}

func TestCircuitBreakerIgnoresCallerCancellationAndCountsOwnTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
		c := &Tasks{log: log, timeout: time.Second, breaker: newBreaker(log, Options{FailureThreshold: 1, OpenTimeout: time.Minute})}
		ctx, cancel := context.WithCancel(t.Context())
		_, err := c.execute(ctx, "get", func(context.Context) ([]byte, error) { cancel(); return nil, context.Canceled })
		if !errors.Is(err, context.Canceled) || c.breaker.State() != gobreaker.StateClosed {
			t.Fatal("caller cancellation tripped breaker")
		}
		_, _ = c.execute(t.Context(), "get", func(ctx context.Context) ([]byte, error) { <-ctx.Done(); return nil, ctx.Err() })
		if c.breaker.State() != gobreaker.StateOpen {
			t.Fatal("cache timeout did not trip breaker")
		}
		time.Sleep(time.Minute + time.Nanosecond)
		_, _ = c.execute(t.Context(), "get", func(context.Context) ([]byte, error) { return nil, errors.New("still down") })
		if c.breaker.State() != gobreaker.StateOpen {
			t.Fatal("failed probe did not reopen breaker")
		}
	})
}
