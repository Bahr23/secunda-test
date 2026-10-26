//go:build integration

package integration_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/repo/cache"
	"github.com/bahr/secunda/internal/repo/persistent"
	"github.com/bahr/secunda/internal/usecase"
	"github.com/bahr/secunda/migrations"
	"github.com/bahr/secunda/pkg/auth"
	"github.com/bahr/secunda/pkg/mysql"
)

var testDB *sql.DB
var testRedis *redis.Client
var testCache *cache.Tasks
var log = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	redisURL := os.Getenv("TEST_REDIS_URL")
	if dsn == "" || redisURL == "" {
		slog.Error("integration tests require TEST_MYSQL_DSN and TEST_REDIS_URL; run make integration")
		os.Exit(1)
	}
	if err := migrations.Up(dsn); err != nil {
		slog.Error("migrations", "error", err)
		os.Exit(1)
	}
	// Reapplying must be safe and preserve existing data.
	if err := migrations.Up(dsn); err != nil {
		slog.Error("repeat migrations", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var err error
	testDB, err = mysql.Open(ctx, dsn, 20, 10)
	if err != nil {
		slog.Error("database", "error", err)
		os.Exit(1)
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		slog.Error("redis config", "error", err)
		os.Exit(1)
	}
	testRedis = redis.NewClient(options)
	if err := testRedis.Ping(ctx).Err(); err != nil {
		slog.Error("redis", "error", err)
		os.Exit(1)
	}
	testCache, err = cache.NewTasks(redisURL, log, cache.Options{Timeout: time.Second})
	if err != nil {
		slog.Error("cache", "error", err)
		os.Exit(1)
	}
	if err := testCache.Ping(ctx); err != nil {
		slog.Error("cache connection", "error", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = testCache.Close()
	_ = testRedis.Close()
	_ = testDB.Close()
	os.Exit(code)
}

type fixture struct {
	store *persistent.Store
	teams *usecase.Team
	tasks *usecase.Task
	team  entity.Team
	owner entity.User
}

func newUser(t *testing.T, store *persistent.Store, name string) entity.User {
	t.Helper()
	u := entity.User{Email: rand.Text() + "@example.test", Name: name, PasswordHash: "fixture-only", CreatedAt: time.Now().UTC()}
	if err := store.Users().Create(t.Context(), &u); err != nil {
		t.Fatal(err)
	}
	return u
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	s := persistent.New(testDB)
	owner := newUser(t, s, "Owner")
	teams := usecase.NewTeam(s.Teams(), s)
	team, err := teams.Create(t.Context(), owner.ID, "Team "+rand.Text())
	if err != nil {
		t.Fatal(err)
	}
	tasks := usecase.NewTask(s.Tasks(), s.Teams(), s, testCache)
	return fixture{store: s, owner: owner, team: team, teams: teams, tasks: tasks}
}

func (f fixture) member(t *testing.T, name string, role entity.Role) entity.User {
	t.Helper()
	u := newUser(t, f.store, name)
	if err := f.teams.Invite(t.Context(), f.owner.ID, f.team.ID, u.ID, role); err != nil {
		t.Fatal(err)
	}
	return u
}

func (f fixture) task(t *testing.T, assignee *int64) entity.Task {
	t.Helper()
	task, err := f.tasks.Create(t.Context(), f.owner.ID, usecase.CreateTask{TeamID: f.team.ID, Title: "Initial", AssigneeID: assignee})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func tokens() *auth.JWT {
	return auth.NewJWT("integration-test-secret-at-least-32-bytes", "test", "test-api", time.Hour)
}
func pointer[T any](v T) *T { return &v }
