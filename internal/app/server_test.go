package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

type runningHTTP struct {
	server   *http.Server
	listener net.Listener
	cancel   context.CancelFunc
	stopped  <-chan error
}

func startHTTP(t *testing.T, handler http.Handler, shutdownTimeout time.Duration) runningHTTP {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() {
		stopped <- serveHTTP(ctx, server, listener, shutdownTimeout, log)
		close(stopped)
	}()
	t.Cleanup(func() {
		cancel()
		_ = server.Close()
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Error("HTTP lifecycle did not exit")
		}
	})
	return runningHTTP{server: server, listener: listener, cancel: cancel, stopped: stopped}
}

type response struct {
	status int
	body   string
	err    error
}

func (s runningHTTP) get(t *testing.T) <-chan response {
	t.Helper()
	result := make(chan response, 1)
	ctx := t.Context()
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		req, err := http.NewRequestWithContext(ctx, "GET", "http://"+s.listener.Addr().String(), nil)
		if err != nil {
			result <- response{err: err}
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			result <- response{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		result <- response{status: resp.StatusCode, body: string(body), err: err}
	}()
	return result
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for HTTP lifecycle")
	}
	var zero T
	return zero
}

func TestServeHTTPDrainsActiveRequests(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	shutdownStarted := make(chan struct{})
	s := startHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
			_, _ = io.WriteString(w, "completed")
		case <-r.Context().Done():
		}
	}), time.Second)
	s.server.RegisterOnShutdown(func() { close(shutdownStarted) })
	result := s.get(t)
	receive(t, entered)
	s.cancel()
	receive(t, shutdownStarted)
	select {
	case err := <-s.stopped:
		t.Fatalf("returned before request completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	resp := receive(t, result)
	if resp.err != nil || resp.status != http.StatusOK || resp.body != "completed" {
		t.Fatalf("response interrupted: %+v", resp)
	}
	if err := receive(t, s.stopped); err != nil {
		t.Fatalf("graceful shutdown: %v", err)
	}
}

func TestServeHTTPForcesCloseAfterShutdownDeadline(t *testing.T) {
	entered := make(chan struct{})
	exited := make(chan struct{})
	s := startHTTP(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(exited)
	}), 30*time.Millisecond)
	result := s.get(t)
	receive(t, entered)
	s.cancel()
	if err := receive(t, s.stopped); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected shutdown deadline, got %v", err)
	}
	receive(t, exited)
	if resp := receive(t, result); resp.err == nil {
		t.Fatalf("timed out request unexpectedly completed: %+v", resp)
	}
}

func TestServeHTTPReportsListenerFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	err = serveHTTP(t.Context(), server, listener, time.Second, log)
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("expected listener failure, got %v", err)
	}
}

func TestServeHTTPClosesListenerWhenContextAlreadyCanceled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	server := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := serveHTTP(ctx, server, listener, time.Second, log); err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("lifecycle returned before closing its listener: %v", err)
	}
}
