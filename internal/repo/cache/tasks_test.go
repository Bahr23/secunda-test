package cache_test

import (
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/repo/cache"
)

func TestCacheBoundsUnresponsiveRedis(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var connections sync.WaitGroup
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Go(func() {
				defer func() { _ = conn.Close() }()
				<-done // Accept TCP but never answer the Redis handshake.
			})
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-accepted
		close(done)
		connections.Wait()
	})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	adapter, err := cache.NewTasks("redis://"+listener.Addr().String(), log, cache.Options{Timeout: 30 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adapter.Close() })
	filter := entity.TaskFilter{TeamID: 1, Pagination: entity.Pagination{Limit: 20}}
	for _, operation := range []struct {
		name string
		run  func()
	}{
		{"ping", func() {
			if err := adapter.Ping(t.Context()); err == nil {
				t.Error("unresponsive server reported healthy")
			}
		}},
		{"get", func() {
			if _, hit := adapter.Get(t.Context(), filter, 1); hit {
				t.Error("unresponsive server returned a hit")
			}
		}},
		{"set", func() { adapter.Set(t.Context(), filter, 1, entity.TaskPage{Items: []entity.Task{}}) }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			start := time.Now()
			operation.run()
			// Allow scheduler/CI overhead while detecting default multi-second retries.
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("cache blocked for %v", elapsed)
			}
		})
	}
}

func TestCacheRejectsInvalidConfigurationWithoutLeakingURL(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		url     string
		timeout time.Duration
	}{
		{"https://user:private-password@example.test", time.Second},
		{"redis://localhost:6379", 0},
		{"redis://localhost:6379", -time.Second},
	} {
		_, err := cache.NewTasks(tc.url, log, cache.Options{Timeout: tc.timeout})
		if err == nil {
			t.Fatalf("accepted invalid configuration: %v", tc.timeout)
		}
		if strings.Contains(err.Error(), "private-password") {
			t.Fatal("configuration error leaked credentials")
		}
	}
}
