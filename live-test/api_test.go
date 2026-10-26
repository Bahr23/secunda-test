//go:build live

package live_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/bahr/secunda/internal/entity"
)

type fixture struct {
	owner, admin, creator, assignee, member, outsider, candidate account
	team, foreign, empty                                         entity.Team
}

func TestLiveAPI(t *testing.T) {
	s := newSuite(t)
	f := fixture{
		owner: s.register(t, "owner"), admin: s.register(t, "admin"),
		creator: s.register(t, "creator"), assignee: s.register(t, "assignee"),
		member: s.register(t, "member"), outsider: s.register(t, "outsider"), candidate: s.register(t, "candidate"),
	}
	f.team, f.foreign, f.empty = s.team(t, f.owner, "main"), s.team(t, f.outsider, "foreign"), s.team(t, f.owner, "empty")
	s.invite(t, f.owner, f.admin, f.team.ID, "admin", 201)
	for _, member := range []account{f.creator, f.assignee, f.member} {
		s.invite(t, f.admin, member, f.team.ID, "", 201)
	}
	s.task(t, f.outsider, f.foreign.ID, "Must never leak", nil)
	t.Run("authentication_and_payloads", func(t *testing.T) { s.authentication(t, f) })
	t.Run("team_visibility_and_roles", func(t *testing.T) { s.roles(t, f) })
	t.Run("task_lifecycle_permissions_and_audit", func(t *testing.T) { s.lifecycle(t, f) })
	t.Run("lists_filters_and_pagination", func(t *testing.T) { s.lists(t, f) })
	t.Run("cursor_pagination", func(t *testing.T) { s.cursors(t, f) })
	t.Run("concurrent_version_conflicts", func(t *testing.T) { s.concurrentUpdates(t, f) })
	t.Run("sql_report", func(t *testing.T) { s.report(t, f) })
	t.Run("redis_failure_and_recovery", func(t *testing.T) { s.redisFailure(t, f) })
	t.Run("rate_limits", func(t *testing.T) { s.rateLimits(t) })
}

func (s *suite) authentication(t *testing.T, f fixture) {
	for _, path := range []string{"/healthz", "/readyz", "/swagger", "/openapi.yaml"} {
		data := s.request(t, "GET", path, account{}, nil, 200)
		if len(data) == 0 {
			t.Fatalf("empty %s", path)
		}
	}
	s.request(t, "GET", "/does-not-exist", account{}, nil, 404)
	s.request(t, "GET", "/api/v1/teams", account{}, nil, 401)
	parts := strings.Split(f.owner.token, ".")
	parts[2] = "A" + parts[2][1:]
	if strings.Join(parts, ".") == f.owner.token {
		parts[2] = "B" + parts[2][1:]
	}
	for _, header := range []string{"Basic abc", "Bearer", "Bearer invalid", "Bearer " + strings.Join(parts, "."), "Bearer " + f.owner.token + " extra"} {
		expect(t, s.raw(t, "GET", "/api/v1/teams", header, "", nil), 401)
	}
	expect(t, s.raw(t, "GET", "/api/v1/teams", "bearer "+f.owner.token, "", nil), 200)
	for _, email := range []string{f.owner.email, s.prefix + ".unknown@example.test"} {
		s.request(t, "POST", "/api/v1/login", account{}, map[string]any{"email": email, "password": "wrong-password"}, 401)
	}
	for _, tc := range []struct{ name, email, password, userName string }{
		{"invalid_email", "invalid", "strong-password-123", "Test"},
		{"display_name_email", "Test <test@example.test>", "strong-password-123", "Test"},
		{"short_password", "test@example.test", "short", "Test"},
		{"long_password", "test@example.test", strings.Repeat("x", 73), "Test"},
		{"empty_name", "test@example.test", "strong-password-123", "  "},
		{"long_name", "test@example.test", "strong-password-123", strings.Repeat("я", 101)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.request(t, "POST", "/api/v1/register", account{}, map[string]any{"email": tc.email, "password": tc.password, "name": tc.userName}, 400)
		})
	}
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
	}{
		{"null", "null", "application/json", 400},
		{"array", "[]", "application/json", 400},
		{"scalar", `"name"`, "application/json", 400},
		{"malformed", `{"name":`, "application/json", 400},
		{"unknown_field", `{"name":"x","owner_id":1}`, "application/json", 400},
		{"second_object", `{"name":"x"} {}`, "application/json", 400},
		{"trailing_garbage", `{"name":"x"} garbage`, "application/json", 400},
		{"missing_content_type", `{"name":"x"}`, "", 415},
		{"text_content_type", `{"name":"x"}`, "text/plain", 415},
		{"body_limit", `{"name":"` + strings.Repeat("x", 1<<20) + `"}`, "application/json", 413},
		{"trailing_whitespace_limit", `{"name":"x"}` + strings.Repeat(" ", 1<<20), "application/json", 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expect(t, s.raw(t, "POST", "/api/v1/teams", "Bearer "+f.owner.token, tc.contentType, []byte(tc.body)), tc.status)
		})
	}
}

func (s *suite) roles(t *testing.T, f fixture) {
	for _, tc := range []struct {
		actor account
		ids   []int64
	}{
		{f.owner, []int64{f.empty.ID, f.team.ID}}, {f.outsider, []int64{f.foreign.ID}}, {f.member, []int64{f.team.ID}},
	} {
		teams := decode[page[entity.Team]](t, s.request(t, "GET", "/api/v1/teams", tc.actor, nil, 200))
		var got []int64
		for _, team := range teams.Items {
			got = append(got, team.ID)
		}
		if !reflect.DeepEqual(got, tc.ids) || teams.Limit != 20 || teams.Offset != 0 {
			t.Fatalf("team visibility/order: %v, want %v", got, tc.ids)
		}
	}
	p := decode[page[entity.Team]](t, s.request(t, "GET", "/api/v1/teams?limit=1&offset=1", f.owner, nil, 200))
	if len(p.Items) != 1 || p.Items[0].ID != f.team.ID || p.Limit != 1 || p.Offset != 1 {
		t.Fatal("team pagination is incorrect")
	}
	for _, name := range []string{" ", strings.Repeat("я", 101)} {
		s.request(t, "POST", "/api/v1/teams", f.owner, map[string]any{"name": name}, 400)
	}
	s.invite(t, f.member, f.candidate, f.team.ID, "member", 403)
	s.invite(t, f.outsider, f.candidate, f.team.ID, "member", 404)
	s.invite(t, f.admin, f.candidate, f.team.ID, "admin", 403)
	s.invite(t, f.owner, f.candidate, f.team.ID, "owner", 400)
	s.invite(t, f.owner, account{id: 9223372036854775807}, f.team.ID, "member", 404)
	s.invite(t, f.owner, f.member, f.team.ID, "admin", 409)
	s.request(t, "GET", statsPath(f.team.ID), f.member, nil, 403)
	s.invite(t, f.admin, f.candidate, f.team.ID, "member", 201)
	rolePath := fmt.Sprintf("/api/v1/teams/%d/members/%d", f.team.ID, f.candidate.id)
	s.request(t, "PATCH", rolePath, f.admin, map[string]any{"role": "admin"}, 403)
	s.request(t, "PATCH", rolePath, f.owner, map[string]any{"role": "admin"}, 200)
	s.request(t, "GET", statsPath(f.team.ID), f.candidate, nil, 200)
	s.request(t, "PATCH", rolePath, f.owner, map[string]any{"role": "member"}, 200)
	s.request(t, "GET", statsPath(f.team.ID), f.candidate, nil, 403) // Same JWT, new permissions.
	s.request(t, "PATCH", rolePath, f.owner, map[string]any{"role": "owner"}, 400)
	s.request(t, "PATCH", fmt.Sprintf("/api/v1/teams/%d/members/%d", f.team.ID, f.owner.id), f.owner, map[string]any{"role": "member"}, 403)
	s.request(t, "PATCH", fmt.Sprintf("/api/v1/teams/%d/members/%d", f.team.ID, f.outsider.id), f.owner, map[string]any{"role": "member"}, 404)
	for _, actor := range []account{f.owner, f.admin} {
		s.request(t, "GET", statsPath(f.team.ID), actor, nil, 200)
	}
	s.request(t, "GET", statsPath(f.team.ID), f.outsider, nil, 404)
}

func (s *suite) lifecycle(t *testing.T, f fixture) {
	task := s.task(t, f.creator, f.team.ID, "  Initial task  ", &f.assignee.id)
	if task.Title != "Initial task" || task.AssigneeID == nil || *task.AssigneeID != f.assignee.id {
		t.Fatal("task normalization or assignment failed")
	}
	path := taskPath(task.ID)
	history := s.history(t, f.creator, task.ID)
	if len(history) != 1 || history[0].ChangedBy != f.creator.id || history[0].Changes["title"].Old != nil || history[0].Changes["title"].New != "Initial task" {
		t.Fatal("creation audit is incorrect")
	}
	s.request(t, "GET", listPath(f.team.ID), f.owner, nil, 200) // Warm cache before the foreign read.
	for _, path := range []string{listPath(f.team.ID), path + "/history", path + "/comments"} {
		s.request(t, "GET", path, f.outsider, nil, 404)
	}
	s.request(t, "POST", "/api/v1/tasks", f.outsider, map[string]any{"team_id": f.team.ID, "title": "intrusion"}, 404)
	s.request(t, "PUT", path, f.outsider, map[string]any{"version": 1, "status": "done"}, 404)
	s.request(t, "POST", path+"/comments", f.outsider, map[string]any{"content": "intrusion"}, 404)
	s.request(t, "PUT", path, f.member, map[string]any{"version": 1, "status": "done"}, 403)
	for _, fields := range []map[string]any{{"title": "denied"}, {"description": "denied"}, {"assignee_id": nil}, {"status": "done", "title": "denied"}} {
		fields["version"] = task.Version
		s.request(t, "PUT", path, f.assignee, fields, 403)
	}
	s.request(t, "PUT", path, f.creator, map[string]any{"version": 1, "assignee_id": f.outsider.id}, 404)
	for _, fields := range []map[string]any{
		{"status": "done"}, {"version": 1}, {"version": 0, "title": "x"}, {"version": 1, "title": " "},
		{"version": 1, "status": "invalid"}, {"version": 1, "title": strings.Repeat("я", 201)},
		{"version": 1, "description": strings.Repeat("я", 10001)}, {"version": 1, "assignee_id": 0},
		{"version": 1, "team_id": f.foreign.ID}, {"version": 1, "created_by": f.owner.id},
	} {
		s.request(t, "PUT", path, f.creator, fields, 400)
	}
	if len(s.history(t, f.owner, task.ID)) != 1 {
		t.Fatal("a rejected update wrote history")
	}
	task = s.update(t, f.assignee, task, map[string]any{"status": "in_progress"})
	if task.Status != entity.StatusInProgress || task.Version != 2 || task.ClosedAt != nil {
		t.Fatal("assignee status update failed")
	}
	task = s.update(t, f.creator, task, map[string]any{"title": "Creator edited", "description": "Creator description"})
	if task.AssigneeID == nil || *task.AssigneeID != f.assignee.id {
		t.Fatal("omitting assignee cleared the assignment")
	}
	task = s.update(t, f.admin, task, map[string]any{"description": "Admin edited"})
	task = s.update(t, f.owner, task, map[string]any{"status": "done"})
	if task.ClosedAt == nil || !task.ClosedAt.Equal(task.UpdatedAt) || task.Version != 5 {
		t.Fatal("closing timestamp/version is incorrect")
	}
	unchanged := s.update(t, f.creator, task, map[string]any{"status": "done", "title": task.Title})
	if !reflect.DeepEqual(unchanged, task) {
		t.Fatal("no-op changed persisted task fields")
	}
	s.request(t, "PUT", path, f.creator, map[string]any{"version": 1, "title": "stale"}, 409)
	if len(s.history(t, f.owner, task.ID)) != 5 {
		t.Fatal("no-op or stale version added an audit row")
	}
	task = s.update(t, f.creator, task, map[string]any{"status": "todo", "assignee_id": nil})
	if task.ClosedAt != nil || task.AssigneeID != nil || task.Version != 6 {
		t.Fatal("reopen or explicit unassignment failed")
	}
	s.request(t, "PUT", path, f.assignee, map[string]any{"version": 6, "status": "done"}, 403)
	history = s.history(t, f.member, task.ID)
	actors := []int64{f.creator.id, f.assignee.id, f.creator.id, f.admin.id, f.owner.id, f.creator.id}
	if len(history) != len(actors) {
		t.Fatalf("audit rows=%d, want=%d", len(history), len(actors))
	}
	for i, row := range history {
		if row.ChangedBy != actors[i] || row.TaskID != task.ID || row.Changes["version"].New != json.Number(fmt.Sprint(i+1)) {
			t.Fatalf("wrong actor/task/version in history row %d", i)
		}
	}
	if history[1].Changes["status"].Old != "todo" || history[1].Changes["status"].New != "in_progress" || history[2].Changes["title"].Old != "Initial task" || history[2].Changes["title"].New != "Creator edited" || history[5].Changes["assignee_id"].New != nil || history[5].Changes["closed_at"].New != nil {
		t.Fatal("audit old/new values do not match accepted changes")
	}
	hp := decode[page[entity.History]](t, s.request(t, "GET", path+"/history?limit=2&offset=1", f.member, nil, 200))
	if len(hp.Items) != 2 || hp.Items[0].ID != history[1].ID || hp.Items[1].ID != history[2].ID {
		t.Fatal("history pagination is incorrect")
	}
	for _, actor := range []account{f.member, f.assignee, f.creator} {
		comment := decode[entity.Comment](t, s.request(t, "POST", path+"/comments", actor, map[string]any{"content": "  Useful comment  "}, 201))
		if comment.Content != "Useful comment" || comment.UserID != actor.id || comment.TaskID != task.ID {
			t.Fatal("comment normalization or attribution failed")
		}
	}
	for _, text := range []string{"  ", strings.Repeat("я", 10001)} {
		s.request(t, "POST", path+"/comments", f.member, map[string]any{"content": text}, 400)
	}
	comments := decode[page[entity.Comment]](t, s.request(t, "GET", path+"/comments?limit=1&offset=1", f.member, nil, 200))
	if len(comments.Items) != 1 || comments.Items[0].UserID != f.assignee.id {
		t.Fatal("comment order/pagination is incorrect")
	}
	if got := s.update(t, f.creator, task, map[string]any{"status": "todo"}); got.Version != task.Version || len(s.history(t, f.owner, task.ID)) != 6 {
		t.Fatal("comments altered the task version or audit")
	}
	for _, suffix := range []string{"/history", "/comments"} {
		s.request(t, "GET", "/api/v1/tasks/9223372036854775807"+suffix, f.owner, nil, 404)
	}
}

func (s *suite) lists(t *testing.T, f fixture) {
	team := s.team(t, f.owner, "pagination")
	s.invite(t, f.owner, f.assignee, team.ID, "member", 201)
	s.request(t, "GET", listPath(team.ID), f.owner, nil, 200) // Cache the empty page.
	first := s.task(t, f.owner, team.ID, "First", &f.assignee.id)
	second := s.task(t, f.owner, team.ID, "Second", nil)
	third := s.task(t, f.owner, team.ID, strings.Repeat("界", 200), &f.assignee.id)
	s.update(t, f.owner, second, map[string]any{"status": "done"})
	s.update(t, f.owner, third, map[string]any{"status": "in_progress", "description": strings.Repeat("界", 10000)})
	for _, tc := range []struct {
		query string
		ids   []int64
	}{
		{"", []int64{third.ID, second.ID, first.ID}},
		{"&status=todo", []int64{first.ID}}, {"&status=done", []int64{second.ID}},
		{"&status=in_progress", []int64{third.ID}},
		{fmt.Sprintf("&assignee_id=%d", f.assignee.id), []int64{third.ID, first.ID}},
		{fmt.Sprintf("&assignee_id=%d&status=todo", f.assignee.id), []int64{first.ID}},
		{"&limit=1&offset=1", []int64{second.ID}}, {"&offset=50", []int64{}},
	} {
		for range 2 { // Assert both the initial query and the cached representation.
			items := decode[page[entity.Task]](t, s.request(t, "GET", listPath(team.ID)+tc.query, f.assignee, nil, 200)).Items
			got := make([]int64, 0, len(items))
			for _, task := range items {
				if task.TeamID != team.ID {
					t.Fatal("list leaked another team's task")
				}
				got = append(got, task.ID)
			}
			if !reflect.DeepEqual(got, tc.ids) || items == nil {
				t.Fatalf("filter %s: ids=%v, want=%v; empty arrays must not be null", tc.query, got, tc.ids)
			}
		}
	}
	for _, query := range []string{"", "?team_id=0", "?team_id=-1", "?team_id=text", "?team_id=9223372036854775808"} {
		s.request(t, "GET", "/api/v1/tasks"+query, f.owner, nil, 400)
	}
	for _, query := range []string{"&limit=0", "&limit=101", "&offset=-1", "&limit=bad", "&status=invalid", "&assignee_id=0", "&assignee_id=", "&assignee_id=text"} {
		s.request(t, "GET", listPath(team.ID)+query, f.owner, nil, 400)
	}
	for _, body := range []map[string]any{
		{"team_id": team.ID, "title": " "}, {"team_id": team.ID, "title": strings.Repeat("x", 201)},
		{"team_id": team.ID, "title": "x", "description": strings.Repeat("x", 10001)},
		{"team_id": team.ID, "title": "x", "assignee_id": -1}, {"team_id": 0, "title": "x"},
		{"team_id": team.ID, "title": "x", "status": "done"},
	} {
		s.request(t, "POST", "/api/v1/tasks", f.owner, body, 400)
	}
	s.request(t, "POST", "/api/v1/tasks", f.owner, map[string]any{"team_id": team.ID, "title": "x", "assignee_id": f.outsider.id}, 404)
	for _, path := range []string{"/api/v1/tasks/zero/history", "/api/v1/tasks/0/comments", "/api/v1/teams/-1/stats"} {
		s.request(t, "GET", path, f.owner, nil, 400)
	}
}

func (s *suite) concurrentUpdates(t *testing.T, f fixture) {
	task := s.task(t, f.creator, f.team.ID, "Before race", nil)
	for round := range 5 {
		start := make(chan struct{})
		var wg sync.WaitGroup
		responses := make([]response, 2)
		for writer := range 2 {
			wg.Go(func() {
				<-start
				body := fmt.Sprintf(`{"version":%d,"title":"round-%d-writer-%d"}`, task.Version, round, writer)
				responses[writer] = s.raw(t, "PUT", taskPath(task.ID), "Bearer "+f.creator.token, "application/json", []byte(body))
			})
		}
		close(start)
		wg.Wait()
		var winner entity.Task
		successes, conflicts := 0, 0
		for _, r := range responses {
			switch r.status {
			case 200:
				winner = decode[entity.Task](t, expect(t, r, 200))
				successes++
			case 409:
				expect(t, r, 409)
				conflicts++
			default:
				t.Fatalf("unexpected concurrent update status=%d", r.status)
			}
		}
		if successes != 1 || conflicts != 1 || winner.Version != task.Version+1 {
			t.Fatalf("lost update: successes=%d conflicts=%d", successes, conflicts)
		}
		history := s.history(t, f.owner, task.ID)
		last := history[len(history)-1]
		if len(history) != round+2 || last.Changes["title"].Old != task.Title || last.Changes["title"].New != winner.Title {
			t.Fatal("concurrent update audit does not match the winning write")
		}
		task = winner
	}
}
