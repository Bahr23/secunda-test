//go:build live

package live_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bahr/secunda/internal/entity"
)

// This suite only talks to a separately running API. It does not construct the
// router or sign its own tokens. Compose access is an explicit, local-only opt-in
// for failure injection, dated report fixtures, and cleanup of this run's data.
type suite struct {
	baseURL  string
	client   *http.Client
	prefix   string
	compose  []string
	root     string
	requests atomic.Int64
	users    []int64
	teams    []int64
}

type account struct {
	id    int64
	email string
	token string
}

type response struct {
	status   int
	header   http.Header
	body     []byte
	duration time.Duration
}

type page[T any] struct {
	Items      []T    `json:"items"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	NextCursor string `json:"next_cursor"`
}

func newSuite(t *testing.T) *suite {
	t.Helper()
	base := strings.TrimRight(os.Getenv("LIVE_API_URL"), "/")
	if base == "" {
		t.Fatal("set LIVE_API_URL to an already running development API")
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		t.Fatal("LIVE_API_URL must be an HTTP(S) origin without credentials")
	}
	s := &suite{baseURL: base, client: &http.Client{Timeout: 5 * time.Second}, prefix: "live-" + strings.ToLower(rand.Text())}
	s.compose = strings.Fields(os.Getenv("LIVE_COMPOSE"))
	if len(s.compose) != 0 && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
		t.Fatal("LIVE_COMPOSE requires a local development API")
	}
	s.root, err = filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if len(s.compose) != 0 {
			s.cleanup(t)
		} else {
			t.Logf("test fixtures retained: prefix=%s users=%v teams=%v", s.prefix, s.users, s.teams)
		}
		t.Logf("real HTTP requests: %d", s.requests.Load())
	})
	return s
}

func (s *suite) rawOnce(t *testing.T, method, path, authorization, contentType string, data []byte) response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, s.baseURL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	start := time.Now()
	resp, err := s.client.Do(req)
	s.requests.Add(1)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		t.Fatal(err)
	}
	r := response{resp.StatusCode, resp.Header, body, time.Since(start)}
	t.Logf("%s %s -> %d (%s)", method, path, r.status, r.duration.Round(time.Millisecond))
	return r
}

// Normal scenarios honor server backpressure. Dedicated limit tests use rawOnce
// to assert the 429 response itself. Only rejected requests are retried.
func (s *suite) raw(t *testing.T, method, path, authorization, contentType string, data []byte) response {
	t.Helper()
	for attempt := 0; ; attempt++ {
		r := s.rawOnce(t, method, path, authorization, contentType, data)
		if r.status != http.StatusTooManyRequests || attempt == 5 {
			return r
		}
		expect(t, r, http.StatusTooManyRequests)
		seconds, err := strconv.Atoi(r.header.Get("Retry-After"))
		if err != nil || seconds < 1 || seconds > 60 {
			t.Fatal("invalid Retry-After")
		}
		time.Sleep(time.Duration(seconds) * time.Second)
	}
}

func expect(t *testing.T, r response, status int) []byte {
	t.Helper()
	if r.status != status {
		// Do not dump successful auth responses: they contain bearer tokens.
		t.Fatalf("HTTP status=%d, want=%d", r.status, status)
	}
	if r.header.Get("X-Request-ID") == "" || r.header.Get("Cache-Control") != "no-store" || r.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("response is missing request ID or cache/content-type safeguards")
	}
	if status >= 400 {
		var problem struct {
			Error struct {
				Code, Message string
				RequestID     string `json:"request_id"`
			}
		}
		if err := json.Unmarshal(r.body, &problem); err != nil {
			t.Fatalf("error response is not JSON: %v", err)
		}
		codes := map[int]string{400: "invalid_input", 401: "unauthenticated", 403: "forbidden", 404: "not_found", 409: "conflict", 413: "body_too_large", 415: "unsupported_media_type", 429: "rate_limited"}
		if problem.Error.Code != codes[status] || problem.Error.Message == "" || problem.Error.RequestID != r.header.Get("X-Request-ID") {
			t.Fatalf("incorrect error envelope: %+v", problem.Error)
		}
	}
	return r.body
}

func (s *suite) request(t *testing.T, method, path string, a account, body any, status int) []byte {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	authorization := ""
	if a.token != "" {
		authorization = "Bearer " + a.token
	}
	return expect(t, s.raw(t, method, path, authorization, "application/json", payload), status)
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func (s *suite) register(t *testing.T, name string) account {
	t.Helper()
	email := s.prefix + "." + name + "@example.test"
	password := "Test-" + rand.Text()
	registration := map[string]any{"email": "  " + strings.ToUpper(email) + " ", "password": password, "name": "  " + name + "  "}
	data := s.request(t, "POST", "/api/v1/register", account{}, registration, 201)
	u := decode[entity.User](t, data)
	s.users = append(s.users, u.ID)
	if u.ID <= 0 || u.Email != email || u.Name != name || bytes.Contains(data, []byte("password")) {
		t.Fatal("registration normalization, identity, or password secrecy failed")
	}
	s.request(t, "POST", "/api/v1/register", account{}, registration, 409)
	credentials := map[string]any{"email": " " + strings.ToUpper(email) + " ", "password": password}
	login := decode[map[string]string](t, s.request(t, "POST", "/api/v1/login", account{}, credentials, 200))
	if login["access_token"] == "" || login["token_type"] != "Bearer" {
		t.Fatal("login did not return a bearer token")
	}
	return account{u.ID, email, login["access_token"]}
}

func (s *suite) team(t *testing.T, owner account, name string) entity.Team {
	t.Helper()
	team := decode[entity.Team](t, s.request(t, "POST", "/api/v1/teams", owner, map[string]any{"name": s.prefix + "-" + name}, 201))
	s.teams = append(s.teams, team.ID)
	if team.CreatedBy != owner.id || team.Role != entity.RoleOwner || team.ID <= 0 {
		t.Fatal("team was not created with its owner")
	}
	return team
}

func (s *suite) invite(t *testing.T, actor, invitee account, teamID int64, role string, status int) {
	t.Helper()
	s.request(t, "POST", fmt.Sprintf("/api/v1/teams/%d/invite", teamID), actor, map[string]any{"user_id": invitee.id, "role": role}, status)
}

func (s *suite) task(t *testing.T, actor account, teamID int64, title string, assignee *int64) entity.Task {
	t.Helper()
	v := decode[entity.Task](t, s.request(t, "POST", "/api/v1/tasks", actor, map[string]any{"team_id": teamID, "title": title, "assignee_id": assignee}, 201))
	if v.ID <= 0 || v.CreatedBy != actor.id || v.TeamID != teamID || v.Status != entity.StatusTodo || v.Version != 1 || v.ClosedAt != nil {
		t.Fatal("incorrect new task defaults")
	}
	return v
}

func taskPath(id int64) string      { return fmt.Sprintf("/api/v1/tasks/%d", id) }
func listPath(teamID int64) string  { return fmt.Sprintf("/api/v1/tasks?team_id=%d", teamID) }
func statsPath(teamID int64) string { return fmt.Sprintf("/api/v1/teams/%d/stats", teamID) }

func (s *suite) update(t *testing.T, actor account, task entity.Task, fields map[string]any) entity.Task {
	t.Helper()
	fields["version"] = task.Version
	return decode[entity.Task](t, s.request(t, "PUT", taskPath(task.ID), actor, fields, 200))
}

func (s *suite) history(t *testing.T, actor account, id int64) []entity.History {
	t.Helper()
	return decode[page[entity.History]](t, s.request(t, "GET", taskPath(id)+"/history?limit=100", actor, nil, 200)).Items
}

func (s *suite) command(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flags := append([]string{}, s.compose[1:]...)
	flags = append(flags, "-p", "secunda", "-f", filepath.Join(s.root, "compose.yaml"))
	cmd := exec.CommandContext(ctx, s.compose[0], append(flags, args...)...)
	cmd.Dir = s.root
	cmd.Stdin = strings.NewReader(stdin)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("local fixture command failed: %v: %s", err, output)
	}
	return strings.TrimSpace(string(output))
}

func (s *suite) sql(t *testing.T, query string) string {
	t.Helper()
	return s.command(t, query, "exec", "-T", "mysql", "sh", "-c", `MYSQL_PWD="$MYSQL_PASSWORD" mysql --batch --skip-column-names -u "$MYSQL_USER" "$MYSQL_DATABASE"`)
}

func ids(values []int64) string {
	parts := []string{"0"}
	for _, id := range values {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}

func (s *suite) cleanup(t *testing.T) {
	t.Helper()
	// Both the random namespace and returned IDs must match. Never truncate or
	// reset the development database, and never touch preexisting user data.
	teamFilter := fmt.Sprintf("SELECT id FROM teams WHERE id IN (%s) AND name LIKE '%s-%%'", ids(s.teams), s.prefix)
	taskFilter := "SELECT id FROM tasks WHERE team_id IN (" + teamFilter + ")"
	s.sql(t, fmt.Sprintf(`START TRANSACTION;
DELETE FROM task_comments WHERE task_id IN (%s);
DELETE FROM task_history WHERE task_id IN (%s);
DELETE FROM tasks WHERE team_id IN (%s);
DELETE FROM team_members WHERE team_id IN (%s);
DELETE FROM teams WHERE id IN (%s) AND name LIKE '%s-%%';
DELETE FROM users WHERE id IN (%s) AND email LIKE '%s.%%@example.test';
COMMIT;`, taskFilter, taskFilter, teamFilter, teamFilter, ids(s.teams), s.prefix, ids(s.users), s.prefix))
	t.Logf("removed only this run's fixtures: %d users, %d teams and their tasks/history/comments", len(s.users), len(s.teams))
}
