//go:build integration

package integration_test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	v1 "github.com/bahr/secunda/internal/controller/http/v1"
	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
	"github.com/bahr/secunda/pkg/auth"
)

type apiClient struct {
	t      *testing.T
	server *httptest.Server
}

func newAPI(t *testing.T, f fixture) apiClient {
	t.Helper()
	a, err := usecase.NewAuth(f.store.Users(), auth.Passwords{Cost: 10}, tokens())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(v1.New(v1.Dependencies{Auth: a, Teams: f.teams, Tasks: f.tasks, Tokens: tokens(), Ready: testDB.PingContext}, v1.Options{Logger: log, RequestTimeout: 10 * time.Second}))
	t.Cleanup(server.Close)
	return apiClient{t, server}
}

func (c apiClient) request(method, path, token string, body any, want int) []byte {
	c.t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
	}
	req, err := http.NewRequestWithContext(c.t.Context(), method, c.server.URL+path, bytes.NewReader(payload))
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.server.Client().Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, resp.StatusCode, want, data)
	}
	if resp.Header.Get("X-Request-ID") == "" {
		c.t.Fatal("request ID missing")
	}
	return data
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return value
}

func tokenFor(t *testing.T, id int64) string {
	t.Helper()
	token, err := tokens().Issue(id)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestAPIAuthenticationAndLifecycle(t *testing.T) {
	f := newFixture(t)
	c := newAPI(t, f)
	email := rand.Text() + "@example.test"
	credentials := map[string]any{"email": email, "password": "a-secure-test-password"}
	registration := map[string]any{"email": email, "password": credentials["password"], "name": "Tester"}
	data := c.request("POST", "/api/v1/register", "", registration, 201)
	if bytes.Contains(data, []byte("password")) {
		t.Fatal("password leaked in user response")
	}
	user := decode[entity.User](t, data)
	c.request("POST", "/api/v1/register", "", registration, 409)
	token := decode[map[string]string](t, c.request("POST", "/api/v1/login", "", credentials, 200))["access_token"]
	c.request("POST", "/api/v1/login", "", map[string]any{"email": email, "password": "wrong-password"}, 401)
	c.request("GET", "/api/v1/teams", "", nil, 401)
	team := decode[entity.Team](t, c.request("POST", "/api/v1/teams", token, map[string]any{"name": "API team"}, 201))
	if team.CreatedBy != user.ID || team.Role != entity.RoleOwner {
		t.Fatalf("owner not established: %+v", team)
	}
	list := decode[struct {
		Items []entity.Team `json:"items"`
	}](t, c.request("GET", "/api/v1/teams", token, nil, 200))
	if len(list.Items) != 1 || list.Items[0].ID != team.ID {
		t.Fatal("team visibility leaked")
	}
	c.request("POST", fmt.Sprintf("/api/v1/teams/%d/invite", team.ID), token, map[string]any{"user_id": f.owner.ID}, 201)
	c.request("PATCH", fmt.Sprintf("/api/v1/teams/%d/members/%d", team.ID, f.owner.ID), token, map[string]any{"role": "admin"}, 200)
	c.request("PATCH", fmt.Sprintf("/api/v1/teams/%d/members/%d", team.ID, user.ID), token, map[string]any{"role": "member"}, 403)
	task := decode[entity.Task](t, c.request("POST", "/api/v1/tasks", token, map[string]any{"team_id": team.ID, "title": "API task", "assignee_id": f.owner.ID}, 201))
	if task.CreatedBy != user.ID || task.Version != 1 {
		t.Fatalf("invalid task: %+v", task)
	}
	path := fmt.Sprintf("/api/v1/tasks/%d", task.ID)
	c.request("PUT", path, token, map[string]any{"status": "done"}, 400)
	updated := decode[entity.Task](t, c.request("PUT", path, token, map[string]any{"version": 1, "status": "done"}, 200))
	if updated.ClosedAt == nil || updated.Version != 2 {
		t.Fatalf("invalid close: %+v", updated)
	}
	c.request("PUT", path, token, map[string]any{"version": 1, "title": "stale"}, 409)
	unchanged := decode[entity.Task](t, c.request("PUT", path, token, map[string]any{"version": 2, "status": "done"}, 200))
	if unchanged.Version != 2 {
		t.Fatal("no-op increased version")
	}
	updated = decode[entity.Task](t, c.request("PUT", path, token, map[string]any{"version": 2, "status": "todo", "assignee_id": nil}, 200))
	if updated.ClosedAt != nil || updated.AssigneeID != nil || updated.Version != 3 {
		t.Fatalf("invalid reopen: %+v", updated)
	}
	c.request("POST", path+"/comments", token, map[string]any{"content": "A useful comment"}, 201)
	comments := decode[struct {
		Items []entity.Comment `json:"items"`
	}](t, c.request("GET", path+"/comments", token, nil, 200))
	if len(comments.Items) != 1 || comments.Items[0].UserID != user.ID {
		t.Fatal("comment attribution wrong")
	}
	history := decode[struct {
		Items []entity.History `json:"items"`
	}](t, c.request("GET", path+"/history", token, nil, 200))
	if len(history.Items) != 3 || history.Items[0].ChangedBy != user.ID {
		t.Fatalf("wrong history: %+v", history)
	}
	c.request("GET", fmt.Sprintf("/api/v1/teams/%d/stats", team.ID), token, nil, 200)
	c.request("GET", "/api/v1/tasks", token, nil, 400)
	c.request("GET", fmt.Sprintf("/api/v1/tasks?team_id=%d&limit=101", team.ID), token, nil, 400)
	c.request("GET", "/healthz", "", nil, 200)
	c.request("GET", "/readyz", "", nil, 200)
	if !strings.Contains(string(c.request("GET", "/openapi.yaml", "", nil, 200)), "openapi:") {
		t.Fatal("OpenAPI missing")
	}
}

func TestAPITeamIsolationAndRoles(t *testing.T) {
	f := newFixture(t)
	admin := f.member(t, "Admin", entity.RoleAdmin)
	creator := f.member(t, "Creator", entity.RoleMember)
	assignee := f.member(t, "Assignee", entity.RoleMember)
	member := f.member(t, "Other member", entity.RoleMember)
	outsider := newUser(t, f.store, "Outsider")
	c := newAPI(t, f)
	task, err := f.tasks.Create(t.Context(), creator.ID, usecase.CreateTask{TeamID: f.team.ID, Title: "Creator task", AssigneeID: &assignee.ID})
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/tasks/%d", task.ID)
	listPath := fmt.Sprintf("/api/v1/tasks?team_id=%d", f.team.ID)
	c.request("GET", listPath, tokenFor(t, f.owner.ID), nil, 200) // Warm shared cache.
	foreignToken := tokenFor(t, outsider.ID)
	c.request("GET", listPath, foreignToken, nil, 404)
	c.request("POST", "/api/v1/tasks", foreignToken, map[string]any{"team_id": f.team.ID, "title": "Intrusion"}, 404)
	c.request("PUT", path, foreignToken, map[string]any{"version": 1, "status": "done"}, 404)
	c.request("GET", path+"/history", foreignToken, nil, 404)
	c.request("GET", path+"/comments", foreignToken, nil, 404)
	c.request("POST", path+"/comments", foreignToken, map[string]any{"content": "Intrusion"}, 404)
	c.request("GET", fmt.Sprintf("/api/v1/teams/%d/stats", f.team.ID), foreignToken, nil, 404)
	c.request("PUT", path, tokenFor(t, member.ID), map[string]any{"version": 1, "status": "done"}, 403)
	c.request("PUT", path, tokenFor(t, assignee.ID), map[string]any{"version": 1, "assignee_id": member.ID}, 403)
	c.request("PUT", path, tokenFor(t, assignee.ID), map[string]any{"version": 1, "title": "Forbidden"}, 403)
	c.request("PUT", path, tokenFor(t, creator.ID), map[string]any{"version": 1, "assignee_id": outsider.ID}, 404)
	c.request("PUT", path, tokenFor(t, assignee.ID), map[string]any{"version": 1, "status": "in_progress"}, 200)
	c.request("PUT", path, tokenFor(t, creator.ID), map[string]any{"version": 2, "title": "Creator edited"}, 200)
	c.request("PUT", path, tokenFor(t, admin.ID), map[string]any{"version": 3, "description": "Admin edited"}, 200)
	c.request("POST", path+"/comments", tokenFor(t, member.ID), map[string]any{"content": "Members can comment"}, 201)
	c.request("GET", fmt.Sprintf("/api/v1/teams/%d/stats", f.team.ID), tokenFor(t, member.ID), nil, 403)
	c.request("POST", fmt.Sprintf("/api/v1/teams/%d/invite", f.team.ID), tokenFor(t, member.ID), map[string]any{"user_id": outsider.ID}, 403)
	c.request("POST", fmt.Sprintf("/api/v1/teams/%d/invite", f.team.ID), tokenFor(t, admin.ID), map[string]any{"user_id": outsider.ID, "role": "owner"}, 400)
}
