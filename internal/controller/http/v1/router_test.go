package v1_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	v1 "github.com/bahr/secunda/internal/controller/http/v1"
	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

type authAdapter struct {
	t        *testing.T
	register func(context.Context, string, string, string) (entity.User, error)
	login    func(context.Context, string, string) (string, error)
}

func (a authAdapter) Register(ctx context.Context, email, password, name string) (entity.User, error) {
	a.t.Helper()
	if a.register == nil {
		a.t.Fatal("unexpected Register call")
	}
	return a.register(ctx, email, password, name)
}

func (a authAdapter) Login(ctx context.Context, email, password string) (string, error) {
	a.t.Helper()
	if a.login == nil {
		a.t.Fatal("unexpected Login call")
	}
	return a.login(ctx, email, password)
}

type teamAdapter struct {
	t    *testing.T
	list func(context.Context, int64, entity.Pagination) (entity.TeamPage, error)
}

func (a teamAdapter) Create(context.Context, int64, string) (entity.Team, error) {
	a.t.Helper()
	a.t.Fatal("unexpected Create team call")
	return entity.Team{}, nil
}
func (a teamAdapter) List(ctx context.Context, actor int64, page entity.Pagination) (entity.TeamPage, error) {
	a.t.Helper()
	if a.list == nil {
		a.t.Fatal("unexpected List teams call")
	}
	return a.list(ctx, actor, page)
}
func (a teamAdapter) Invite(context.Context, int64, int64, int64, entity.Role) error {
	a.t.Helper()
	a.t.Fatal("unexpected Invite call")
	return nil
}
func (a teamAdapter) ChangeRole(context.Context, int64, int64, int64, entity.Role) error {
	a.t.Helper()
	a.t.Fatal("unexpected ChangeRole call")
	return nil
}
func (a teamAdapter) Stats(context.Context, int64, int64) (entity.TeamStats, error) {
	a.t.Helper()
	a.t.Fatal("unexpected Stats call")
	return entity.TeamStats{}, nil
}

type taskAdapter struct {
	t      *testing.T
	update func(context.Context, int64, int64, usecase.UpdateTask) (entity.Task, error)
}

func (a taskAdapter) Create(context.Context, int64, usecase.CreateTask) (entity.Task, error) {
	a.t.Helper()
	a.t.Fatal("unexpected Create task call")
	return entity.Task{}, nil
}
func (a taskAdapter) List(context.Context, int64, entity.TaskFilter) (entity.TaskPage, error) {
	a.t.Helper()
	a.t.Fatal("unexpected List tasks call")
	return entity.TaskPage{}, nil
}
func (a taskAdapter) Update(ctx context.Context, actor, task int64, update usecase.UpdateTask) (entity.Task, error) {
	a.t.Helper()
	if a.update == nil {
		a.t.Fatal("unexpected Update call")
	}
	return a.update(ctx, actor, task, update)
}
func (a taskAdapter) History(context.Context, int64, int64, entity.Pagination) (entity.HistoryPage, error) {
	a.t.Helper()
	a.t.Fatal("unexpected History call")
	return entity.HistoryPage{}, nil
}
func (a taskAdapter) Comment(context.Context, int64, int64, string) (entity.Comment, error) {
	a.t.Helper()
	a.t.Fatal("unexpected Comment call")
	return entity.Comment{}, nil
}
func (a taskAdapter) Comments(context.Context, int64, int64, entity.Pagination) (entity.CommentPage, error) {
	a.t.Helper()
	a.t.Fatal("unexpected Comments call")
	return entity.CommentPage{}, nil
}

type tokenAdapter struct{}

func (tokenAdapter) Verify(raw string) (int64, error) {
	if raw == "valid-token" {
		return 42, nil
	}
	return 0, entity.ErrUnauthenticated
}

func dependencies(t *testing.T) v1.Dependencies {
	t.Helper()
	return v1.Dependencies{
		Auth: authAdapter{t: t}, Teams: teamAdapter{t: t}, Tasks: taskAdapter{t: t},
		Tokens: tokenAdapter{}, Ready: func(context.Context) error { return nil },
	}
}

func router(deps v1.Dependencies, output io.Writer) http.Handler {
	return v1.New(deps, v1.Options{
		Logger: slog.New(slog.NewJSONHandler(output, nil)), RequestTimeout: time.Second,
	})
}

func request(handler http.Handler, method, path, authorization, contentType, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", authorization)
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func assertError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var body struct {
		Error struct {
			Code, Message string
			RequestID     string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, response.Body)
	}
	if response.Code != status || body.Error.Code != code {
		t.Fatalf("status=%d code=%q, want %d %q; body=%s", response.Code, body.Error.Code, status, code, response.Body)
	}
	if body.Error.RequestID == "" || body.Error.RequestID != response.Header().Get("X-Request-ID") {
		t.Fatal("error and response header must share a request ID")
	}
}

func TestRouterAuthenticationAndRequestContext(t *testing.T) {
	for _, authorization := range []string{"", "Basic valid-token", "Bearer", "Bearer invalid-token", "Bearer valid-token extra"} {
		t.Run(authorization, func(t *testing.T) {
			response := request(router(dependencies(t), io.Discard), "GET", "/api/v1/teams", authorization, "", "")
			assertError(t, response, http.StatusUnauthorized, "unauthenticated")
		})
	}

	deps := dependencies(t)
	called := false
	deps.Teams = teamAdapter{t: t, list: func(ctx context.Context, actor int64, page entity.Pagination) (entity.TeamPage, error) {
		called = true
		if actor != 42 || page.Limit != 7 || page.Offset != 3 {
			t.Fatalf("actor=%d page=%+v", actor, page)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > time.Second {
			t.Fatalf("missing request deadline: %v %v", deadline, ok)
		}
		return entity.TeamPage{Items: []entity.Team{}, Pagination: page}, nil
	}}
	var output bytes.Buffer
	response := request(router(deps, &output), "GET", "/api/v1/teams?limit=7&offset=3", "Bearer valid-token", "", "")
	if response.Code != 200 || !called {
		t.Fatalf("status=%d called=%v", response.Code, called)
	}
	requestID := response.Header().Get("X-Request-ID")
	if requestID == "" || !strings.Contains(output.String(), requestID) {
		t.Fatal("request ID must correlate response and access log")
	}
	if strings.Contains(output.String(), "valid-token") || strings.Contains(output.String(), "offset=3") {
		t.Fatal("access log exposed credentials or query string")
	}
}

func TestRouterApplicationErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid", fmt.Errorf("wrapped: %w: invalid page", entity.ErrInvalid), 400, "invalid_input"},
		{"unauthenticated", entity.ErrUnauthenticated, 401, "unauthenticated"},
		{"forbidden", entity.ErrForbidden, 403, "forbidden"},
		{"not found", entity.ErrNotFound, 404, "not_found"},
		{"conflict", entity.ErrConflict, 409, "conflict"},
		{"timeout", context.DeadlineExceeded, 504, "timeout"},
		{"internal", errors.New("private database detail"), 500, "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := dependencies(t)
			deps.Teams = teamAdapter{t: t, list: func(context.Context, int64, entity.Pagination) (entity.TeamPage, error) {
				return entity.TeamPage{}, tc.err
			}}
			response := request(router(deps, io.Discard), "GET", "/api/v1/teams", "Bearer valid-token", "", "")
			assertError(t, response, tc.status, tc.code)
			if strings.Contains(response.Body.String(), "private database detail") {
				t.Fatal("internal error details leaked")
			}
		})
	}
}

func TestRouterRejectsMalformedBodiesBeforeCallingApplication(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
		code                    string
	}{
		{"unknown field", `{"name":"test","created_by":1}`, "application/json", 400, "invalid_input"},
		{"trailing object", `{"name":"test"} {}`, "application/json", 400, "invalid_input"},
		{"malformed", `{"name":`, "application/json", 400, "invalid_input"},
		{"null", `null`, "application/json", 400, "invalid_input"},
		{"array", `[]`, "application/json", 400, "invalid_input"},
		{"wrong media type", `{}`, "text/plain", 415, "unsupported_media_type"},
		{"too large", `{"name":"` + strings.Repeat("a", 1<<20) + `"}`, "application/json", 413, "body_too_large"},
		{"oversized trailing whitespace", `{}` + strings.Repeat(" ", 1<<20), "application/json", 413, "body_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := request(router(dependencies(t), io.Discard), "POST", "/api/v1/register", "", tc.contentType, tc.body)
			assertError(t, response, tc.status, tc.code)
		})
	}
}

func TestRouterRegistrationPreservesApplicationValues(t *testing.T) {
	deps := dependencies(t)
	deps.Auth = authAdapter{t: t, register: func(_ context.Context, email, password, name string) (entity.User, error) {
		if email != "person@example.test" || password != "test-password" || name != "Name" {
			t.Fatal("registration fields changed in transport")
		}
		return entity.User{ID: 1, Email: email, Name: name, PasswordHash: "private-hash"}, nil
	}}
	response := request(router(deps, io.Discard), "POST", "/api/v1/register", "", "application/json", `{"email":"person@example.test","password":"test-password","name":"Name"}`)
	if response.Code != 201 {
		t.Fatalf("status=%d", response.Code)
	}
	if strings.Contains(response.Body.String(), "password") || strings.Contains(response.Body.String(), "private-hash") {
		t.Fatal("password hash leaked in response")
	}
}

func TestRouterTaskUpdateDistinguishesOmittedAndNullAssignee(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		set         bool
		value       int64
	}{
		{"omitted", "", false, 0}, {"unassign", `,"assignee_id":null`, true, 0}, {"assign", `,"assignee_id":7`, true, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := dependencies(t)
			called := false
			deps.Tasks = taskAdapter{t: t, update: func(_ context.Context, actor, taskID int64, update usecase.UpdateTask) (entity.Task, error) {
				called = true
				if actor != 42 || taskID != 13 || update.Version != 2 || update.SetAssignee != tc.set {
					t.Fatalf("actor=%d task=%d update=%+v", actor, taskID, update)
				}
				if tc.value == 0 && update.AssigneeID != nil {
					t.Fatal("expected no assignee value")
				}
				if tc.value != 0 && (update.AssigneeID == nil || *update.AssigneeID != tc.value) {
					t.Fatal("wrong assignee")
				}
				return entity.Task{ID: taskID}, nil
			}}
			response := request(router(deps, io.Discard), "PUT", "/api/v1/tasks/13", "Bearer valid-token", "application/json", `{"version":2,"status":"done"`+tc.field+`}`)
			if response.Code != 200 || !called {
				t.Fatalf("status=%d called=%v", response.Code, called)
			}
		})
	}
}

func TestRouterReadinessAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		readyErr           error
		status             int
		code               string
	}{
		{"healthy", "GET", "/readyz", nil, 200, ""},
		{"unavailable", "GET", "/readyz", errors.New("private connection detail"), 503, "unavailable"},
		{"liveness independent of database", "GET", "/healthz", errors.New("offline"), 200, ""},
		{"unknown route", "GET", "/missing", nil, 404, "not_found"},
		{"unsupported method", "DELETE", "/api/v1/teams", nil, 404, "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := dependencies(t)
			deps.Ready = func(context.Context) error { return tc.readyErr }
			response := request(router(deps, io.Discard), tc.method, tc.path, "", "", "")
			if tc.code != "" {
				assertError(t, response, tc.status, tc.code)
			} else if response.Code != tc.status {
				t.Fatalf("status=%d", response.Code)
			}
			if strings.Contains(response.Body.String(), "private connection detail") {
				t.Fatal("readiness leaked connection details")
			}
		})
	}
}

func TestRouterRecoversApplicationPanic(t *testing.T) {
	deps := dependencies(t)
	deps.Teams = teamAdapter{t: t, list: func(context.Context, int64, entity.Pagination) (entity.TeamPage, error) {
		panic("private panic detail")
	}}
	var output bytes.Buffer
	response := request(router(deps, &output), "GET", "/api/v1/teams", "Bearer valid-token", "", "")
	assertError(t, response, 500, "internal_error")
	if strings.Contains(response.Body.String(), "private panic detail") {
		t.Fatal("panic detail leaked")
	}
	if !strings.Contains(output.String(), "request panic") {
		t.Fatal("panic was not logged")
	}
}
