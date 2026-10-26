package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
	"github.com/bahr/secunda/pkg/logger"
)

const TaskTTL = 5 * time.Minute

type Tasks struct {
	client  *redis.Client
	log     *slog.Logger
	timeout time.Duration
	breaker *gobreaker.CircuitBreaker[[]byte]
}

// NewTasks owns the Redis client and its fail-open policy. Callers supply only
// connection configuration; an unavailable Redis server does not prevent startup.
func NewTasks(url string, log *slog.Logger, settings Options) (*Tasks, error) {
	if settings.Timeout <= 0 {
		return nil, fmt.Errorf("cache timeout must be positive")
	}
	if settings.FailureThreshold == 0 {
		settings.FailureThreshold = 5
	}
	if settings.OpenTimeout == 0 {
		settings.OpenTimeout = 10 * time.Second
	}
	if settings.OpenTimeout < 0 {
		return nil, fmt.Errorf("breaker timeout must be positive")
	}
	timeout := settings.Timeout
	options, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("invalid Redis URL")
	}
	options.ContextTimeoutEnabled = true
	options.MaxRetries = -1
	options.DialTimeout = timeout
	options.ReadTimeout = timeout
	options.WriteTimeout = timeout
	options.PoolTimeout = timeout
	return &Tasks{client: redis.NewClient(options), log: log, timeout: timeout, breaker: newBreaker(log, settings)}, nil
}

func (c *Tasks) Close() error { return c.client.Close() }

func (c *Tasks) Ping(ctx context.Context) error {
	_, err := c.execute(ctx, "ping", func(ctx context.Context) ([]byte, error) { return nil, c.client.Ping(ctx).Err() })
	return err
}

func taskKey(f entity.TaskFilter, revision int64) string {
	assignee := "any"
	if f.AssigneeID != nil {
		assignee = fmt.Sprint(*f.AssigneeID)
	}
	key := fmt.Sprintf("secunda:tasks:v1:team:%d:revision:%d:status:%s:assignee:%s:limit:%d:offset:%d", f.TeamID, revision, f.Status, assignee, f.Limit, f.Offset)
	if f.Cursor {
		key += fmt.Sprintf(":cursor:after:%d", f.AfterID)
	}
	return key
}

func (c *Tasks) Get(ctx context.Context, f entity.TaskFilter, revision int64) (entity.TaskPage, bool) {
	var page entity.TaskPage
	data, err := c.execute(ctx, "get", func(ctx context.Context) ([]byte, error) { return c.client.Get(ctx, taskKey(f, revision)).Bytes() })
	if err != nil {
		return page, false
	}
	err = json.Unmarshal(data, &page)
	if err != nil {
		logger.FromContext(ctx, c.log).WarnContext(ctx, "invalid task cache entry", slog.String("event", "cache.invalid_entry"), slog.Any("error", err))
		return page, false
	}
	return page, true
}

func (c *Tasks) Set(ctx context.Context, f entity.TaskFilter, revision int64, page entity.TaskPage) {
	data, err := json.Marshal(page)
	if err != nil {
		c.log.ErrorContext(ctx, "encode task cache", slog.Any("error", err))
		return
	}
	_, _ = c.execute(ctx, "set", func(ctx context.Context) ([]byte, error) {
		return nil, c.client.Set(ctx, taskKey(f, revision), data, TaskTTL).Err()
	})
}

var _ usecase.TaskCache = (*Tasks)(nil)
