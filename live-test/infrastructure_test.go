//go:build live

package live_test

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bahr/secunda/internal/entity"
)

func (s *suite) report(t *testing.T, f fixture) {
	empty := decode[entity.TeamStats](t, s.request(t, "GET", statsPath(f.empty.ID), f.owner, nil, 200))
	if !reflect.DeepEqual(empty.TasksByStatus, map[entity.Status]int64{"todo": 0, "in_progress": 0, "done": 0}) || empty.AverageCloseSeconds != nil || empty.TopAssignees == nil || len(empty.TopAssignees) != 0 || empty.CommentCount != 0 {
		t.Fatal("empty team report is incorrect")
	}
	team := s.team(t, f.owner, "report")
	performers := []account{f.creator, f.assignee, f.member, f.candidate}
	for _, a := range performers {
		s.invite(t, f.owner, a, team.ID, "member", 201)
	}
	s.invite(t, f.owner, f.admin, team.ID, "admin", 201)
	var closed []entity.Task
	for i, a := range performers {
		count := 2
		if i == 0 {
			count = 3
		}
		for n := range count {
			task := s.task(t, f.owner, team.ID, fmt.Sprintf("Performer %d task %d", i, n), &a.id)
			closed = append(closed, s.update(t, f.owner, task, map[string]any{"status": "done"}))
		}
	}
	unassigned := s.task(t, f.owner, team.ID, "Unassigned done task", nil)
	closed = append(closed, s.update(t, f.owner, unassigned, map[string]any{"status": "done"}))
	open := s.task(t, f.owner, team.ID, "Still open", &f.creator.id)
	inProgress := s.task(t, f.owner, team.ID, "Working", &f.assignee.id)
	s.update(t, f.owner, inProgress, map[string]any{"status": "in_progress"})
	for _, id := range []int64{closed[0].ID, closed[0].ID, closed[1].ID, open.ID} {
		s.request(t, "POST", taskPath(id)+"/comments", f.member, map[string]any{"content": "Report comment"}, 201)
	}
	stats := decode[entity.TeamStats](t, s.request(t, "GET", statsPath(team.ID), f.admin, nil, 200))
	if stats.TeamID != team.ID || !reflect.DeepEqual(stats.TasksByStatus, map[entity.Status]int64{"todo": 1, "in_progress": 1, "done": 10}) || stats.CommentCount != 4 {
		t.Fatalf("report counts are incorrect: %+v", stats)
	}
	if len(stats.TopAssignees) != 3 || stats.TopAssignees[0].UserID != f.creator.id || stats.TopAssignees[0].ClosedTasks != 3 || stats.TopAssignees[1].UserID != f.assignee.id || stats.TopAssignees[1].ClosedTasks != 2 || stats.TopAssignees[2].UserID != f.member.id || stats.TopAssignees[2].ClosedTasks != 2 {
		t.Fatalf("top-three ranking/tie ordering is incorrect: %+v", stats.TopAssignees)
	}
	var seconds float64
	for _, task := range closed {
		seconds += task.ClosedAt.Sub(task.CreatedAt).Seconds()
	}
	checkAverage(t, stats, seconds/float64(len(closed)))
	s.request(t, "GET", statsPath(team.ID), f.member, nil, 403)
	s.request(t, "GET", statsPath(team.ID), f.outsider, nil, 404)
	// Reopening removes the task from both the ranking and the closing average.
	s.update(t, f.owner, closed[0], map[string]any{"status": "todo"})
	seconds -= closed[0].ClosedAt.Sub(closed[0].CreatedAt).Seconds()
	stats = decode[entity.TeamStats](t, s.request(t, "GET", statsPath(team.ID), f.owner, nil, 200))
	if stats.TasksByStatus["done"] != 9 || stats.TasksByStatus["todo"] != 2 || stats.TopAssignees[0].ClosedTasks != 2 || stats.CommentCount != 4 {
		t.Fatal("reopening did not update the report correctly")
	}
	checkAverage(t, stats, seconds/9)
	t.Run("thirty_day_window", func(t *testing.T) {
		if len(s.compose) == 0 {
			t.Skip("LIVE_COMPOSE is required to date an isolated fixture beyond 30 days")
		}
		// HTTP intentionally cannot backdate tasks. Only this run's isolated
		// fixture is dated in SQL; the resulting report is still requested over HTTP.
		old := closed[1]
		s.sql(t, fmt.Sprintf(`UPDATE tasks t JOIN teams tm ON tm.id=t.team_id
SET t.created_at=UTC_TIMESTAMP(6)-INTERVAL 32 DAY,
    t.closed_at=UTC_TIMESTAMP(6)-INTERVAL 31 DAY
WHERE t.id=%d AND tm.id=%d AND tm.name='%s-report';`, old.ID, team.ID, s.prefix))
		stats = decode[entity.TeamStats](t, s.request(t, "GET", statsPath(team.ID), f.owner, nil, 200))
		if stats.TasksByStatus["done"] != 9 || len(stats.TopAssignees) != 3 || stats.TopAssignees[0].UserID != f.assignee.id || stats.TopAssignees[1].UserID != f.member.id || stats.TopAssignees[2].UserID != f.candidate.id {
			t.Fatal("30-day ranking included the old closure or changed total status counts")
		}
		seconds += 86400 - old.ClosedAt.Sub(old.CreatedAt).Seconds()
		checkAverage(t, stats, seconds/9) // All-time average must still include it.
	})
}

func checkAverage(t *testing.T, stats entity.TeamStats, want float64) {
	t.Helper()
	if stats.AverageCloseSeconds == nil || math.Abs(*stats.AverageCloseSeconds-want) > 0.00001 {
		t.Fatalf("average closing time=%v, want=%.9f", stats.AverageCloseSeconds, want)
	}
}

func (s *suite) redisFailure(t *testing.T, f fixture) {
	if len(s.compose) == 0 {
		t.Skip("set LIVE_COMPOSE to enable a temporary pause of the local Redis container")
	}
	team := s.team(t, f.owner, "redis")
	task := s.task(t, f.owner, team.ID, "Before outage", nil)
	path := listPath(team.ID)
	s.request(t, "GET", path, f.owner, nil, 200)
	key := fmt.Sprintf("secunda:tasks:v1:team:%d:revision:1:status::assignee:any:limit:20:offset:0", team.ID)
	ttl, err := strconv.Atoi(s.command(t, "", "exec", "-T", "redis", "redis-cli", "TTL", key))
	if err != nil || ttl <= 0 || ttl > 300 {
		t.Fatalf("expected a cache entry with a five-minute TTL, got %d (%v)", ttl, err)
	}
	// A malformed cache payload must trigger a DB read and repair, not a 500.
	s.command(t, "", "exec", "-T", "redis", "redis-cli", "SET", key, "{invalid", "EX", "300")
	items := decode[page[entity.Task]](t, s.request(t, "GET", path, f.owner, nil, 200)).Items
	if len(items) != 1 || items[0].Title != task.Title {
		t.Fatal("corrupt cache did not fall back to SQL")
	}
	if cached := s.command(t, "", "exec", "-T", "redis", "redis-cli", "GET", key); !strings.Contains(cached, "Before outage") {
		t.Fatal("corrupt cache was not repaired")
	}
	// Pause preserves old entries and creates an unresponsive TCP dependency,
	// which is stronger than connection refusal from a stopped container.
	s.command(t, "", "pause", "redis")
	paused := true
	t.Cleanup(func() {
		if paused {
			s.command(t, "", "unpause", "redis")
		}
	})
	r := s.raw(t, "GET", path, "Bearer "+f.owner.token, "", nil)
	items = decode[page[entity.Task]](t, expect(t, r, 200)).Items
	if len(items) != 1 || items[0].Title != task.Title || r.duration > 1500*time.Millisecond {
		t.Fatalf("Redis outage stalled or changed the SQL fallback: %s", r.duration)
	}
	s.request(t, "GET", "/readyz", account{}, nil, 200)
	task = s.update(t, f.owner, task, map[string]any{"title": "Changed during outage", "status": "done"})
	second := s.task(t, f.owner, team.ID, "Created during outage", nil)
	items = decode[page[entity.Task]](t, s.request(t, "GET", path, f.owner, nil, 200)).Items
	if len(items) != 2 || items[0].ID != second.ID || items[1].Title != task.Title {
		t.Fatal("writes during the Redis outage were not visible")
	}
	// Five failed Redis operations open the default breaker. Further reads must
	// bypass the unresponsive TCP connection instead of waiting on each timeout.
	s.request(t, "GET", path, f.owner, nil, 200)
	fast := s.raw(t, "GET", path, "Bearer "+f.owner.token, "", nil)
	expect(t, fast, 200)
	if fast.duration > 120*time.Millisecond {
		t.Fatalf("open breaker still waited for Redis: %s", fast.duration)
	}
	t.Logf("open breaker bypassed Redis in %s", fast.duration.Round(time.Millisecond))
	s.command(t, "", "unpause", "redis")
	paused = false
	if pong := s.command(t, "", "exec", "-T", "redis", "redis-cli", "PING"); pong != "PONG" {
		t.Fatal("Redis did not recover")
	}
	for range 2 {
		items = decode[page[entity.Task]](t, s.request(t, "GET", path, f.owner, nil, 200)).Items
		if len(items) != 2 || items[1].Title != task.Title || items[1].Status != entity.StatusDone {
			t.Fatal("old cached data resurfaced after Redis recovered")
		}
	}
	newKey := strings.Replace(key, ":revision:1:", ":revision:3:", 1)
	deadline := time.Now().Add(30 * time.Second)
	for {
		cached := s.command(t, "", "exec", "-T", "redis", "redis-cli", "GET", newKey)
		if strings.Contains(cached, "Changed during outage") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("breaker did not recover and fill the new cache revision")
		}
		time.Sleep(500 * time.Millisecond)
		s.request(t, "GET", path, f.owner, nil, 200)
	}
	t.Logf("Redis unresponsive: SQL fallback returned in %s; service remained ready", r.duration.Round(time.Millisecond))
}
