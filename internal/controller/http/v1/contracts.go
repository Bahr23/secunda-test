package v1

import (
	"context"
	"log/slog"
	"time"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

// Application contracts are owned by the HTTP caller. Tests exercise the same
// router with in-process adapters, without constructing SQL or Redis clients.
type Authentication interface {
	Register(context.Context, string, string, string) (entity.User, error)
	Login(context.Context, string, string) (string, error)
}

type TeamOperations interface {
	Create(context.Context, int64, string) (entity.Team, error)
	List(context.Context, int64, entity.Pagination) (entity.TeamPage, error)
	Invite(context.Context, int64, int64, int64, entity.Role) error
	ChangeRole(context.Context, int64, int64, int64, entity.Role) error
	Stats(context.Context, int64, int64) (entity.TeamStats, error)
}

type TaskOperations interface {
	Create(context.Context, int64, usecase.CreateTask) (entity.Task, error)
	List(context.Context, int64, entity.TaskFilter) (entity.TaskPage, error)
	Update(context.Context, int64, int64, usecase.UpdateTask) (entity.Task, error)
	History(context.Context, int64, int64, entity.Pagination) (entity.HistoryPage, error)
	Comment(context.Context, int64, int64, string) (entity.Comment, error)
	Comments(context.Context, int64, int64, entity.Pagination) (entity.CommentPage, error)
}

type TokenVerifier interface {
	Verify(string) (int64, error)
}

type Dependencies struct {
	Auth   Authentication
	Teams  TeamOperations
	Tasks  TaskOperations
	Tokens TokenVerifier
	Ready  func(context.Context) error
}

type Options struct {
	Logger         *slog.Logger
	RequestTimeout time.Duration
	CursorSecret   []byte
	RateLimit      *RateLimitOptions
}
