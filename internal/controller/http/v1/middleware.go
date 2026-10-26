package v1

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/pkg/logger"
)

type actorKey struct{}
type observationKey struct{}
type observation struct{ userID int64 }

func actor(r *http.Request) int64 { id, _ := r.Context().Value(actorKey{}).(int64); return id }

func (h *handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			h.fail(w, r, entity.ErrUnauthenticated)
			return
		}
		id, err := h.tokens.Verify(parts[1])
		if err != nil {
			h.fail(w, r, entity.ErrUnauthenticated)
			return
		}
		if state, ok := r.Context().Value(observationKey{}).(*observation); ok {
			state.userID = id
		}
		ctx := context.WithValue(r.Context(), actorKey{}, id)
		ctx = logger.WithContext(ctx, logger.FromContext(ctx, h.log).With(slog.Int64("user_id", id)))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (h *handler) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := rand.Text()
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		log := h.log.With(slog.String("request_id", requestID))
		state := &observation{}
		ctx, cancel := context.WithTimeout(context.WithValue(logger.WithContext(r.Context(), log), observationKey{}, state), h.timeout)
		defer cancel()
		r = r.WithContext(ctx)
		rw := &responseWriter{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				log.ErrorContext(ctx, "request panic", slog.Any("panic", recovered), slog.String("stack", string(debug.Stack())))
				if rw.status == 0 {
					writeError(rw, 500, "internal_error", "internal server error")
				}
			}
			status := rw.status
			if status == 0 {
				status = 200
			}
			level := slog.LevelInfo
			if status >= 500 {
				level = slog.LevelError
			} else if status >= 400 {
				level = slog.LevelWarn
			}
			attributes := []slog.Attr{slog.String("event", "http.request"), slog.String("method", r.Method),
				slog.String("route", r.Pattern), slog.Int("status", status), slog.Float64("duration_ms", float64(time.Since(start))/float64(time.Millisecond))}
			if state.userID > 0 {
				attributes = append(attributes, slog.Int64("user_id", state.userID))
			}
			log.LogAttrs(ctx, level, "http request", attributes...)
		}()
		next.ServeHTTP(rw, r)
	})
}
