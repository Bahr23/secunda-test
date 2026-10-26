package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// serveHTTP owns the listener and drains active requests before the caller
// releases their dependencies. Shutdown gets its own context after cancellation.
func serveHTTP(ctx context.Context, server *http.Server, listener net.Listener, shutdownTimeout time.Duration, log *slog.Logger) error {
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	log.Info("HTTP server started", slog.String("address", listener.Addr().String()))

	var serveErr error
	serveFinished := false
	select {
	case serveErr = <-served:
		serveFinished = true
	case <-ctx.Done():
	}

	log.Info("HTTP server shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	shutdownErr := server.Shutdown(shutdown)
	if shutdownErr != nil {
		_ = server.Close()
		shutdownErr = fmt.Errorf("shutdown HTTP server: %w", shutdownErr)
	}
	if !serveFinished {
		serveErr = <-served
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = fmt.Errorf("serve HTTP: %w", serveErr)
	} else {
		serveErr = nil
	}
	return errors.Join(serveErr, shutdownErr)
}
