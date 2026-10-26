package cache

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"

	"github.com/bahr/secunda/pkg/logger"
)

var errCallerCanceled = errors.New("cache caller canceled")

type Options struct {
	Timeout          time.Duration
	FailureThreshold uint32
	OpenTimeout      time.Duration
}

func newBreaker(log *slog.Logger, options Options) *gobreaker.CircuitBreaker[[]byte] {
	return gobreaker.NewCircuitBreaker[[]byte](gobreaker.Settings{
		Name: "redis", MaxRequests: 1, Timeout: options.OpenTimeout,
		ReadyToTrip:  func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= options.FailureThreshold },
		IsSuccessful: func(err error) bool { return err == nil || errors.Is(err, redis.Nil) },
		IsExcluded:   func(err error) bool { return errors.Is(err, errCallerCanceled) },
		OnStateChange: func(name string, from, to gobreaker.State) {
			level := slog.LevelInfo
			if to == gobreaker.StateOpen {
				level = slog.LevelWarn
			}
			log.LogAttrs(context.Background(), level, "cache circuit breaker changed state",
				slog.String("event", "circuit_breaker.state_change"), slog.String("dependency", name),
				slog.String("from", from.String()), slog.String("to", to.String()))
		},
	})
}

func (c *Tasks) execute(ctx context.Context, operation string, fn func(context.Context) ([]byte, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := c.breaker.Execute(func() ([]byte, error) {
		bounded, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()
		data, err := fn(bounded)
		if ctx.Err() != nil {
			return nil, errors.Join(errCallerCanceled, ctx.Err())
		}
		return data, err
	})
	if err != nil && !errors.Is(err, redis.Nil) && !errors.Is(err, errCallerCanceled) {
		level := slog.LevelWarn
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			level = slog.LevelDebug
		}
		logger.FromContext(ctx, c.log).LogAttrs(ctx, level, "cache operation unavailable",
			slog.String("event", "cache.unavailable"), slog.String("dependency", "redis"),
			slog.String("operation", operation), slog.Any("error", err))
	}
	return data, err
}
