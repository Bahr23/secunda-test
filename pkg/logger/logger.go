package logger

import (
	"context"
	"io"
	"log/slog"
)

type contextKey struct{}

func New(out io.Writer, level, format string) (*slog.Logger, error) {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		return nil, err
	}
	options := &slog.HandlerOptions{Level: parsed}
	var handler slog.Handler = slog.NewJSONHandler(out, options)
	if format == "text" {
		handler = slog.NewTextHandler(out, options)
	}
	return slog.New(handler).With(slog.String("service", "secunda")), nil
}

func WithContext(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, log)
}

func FromContext(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if log, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return log
	}
	return fallback
}
