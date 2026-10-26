//go:build integration

package integration_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

func TestSQLTeamReport(t *testing.T) {
	f := newFixture(t)
	alice := f.member(t, "Alice", entity.RoleAdmin)
	bob := f.member(t, "Bob", entity.RoleMember)
	carol := f.member(t, "Carol", entity.RoleMember)
	dan := f.member(t, "Dan", entity.RoleMember)
	other := newFixture(t)
	var now time.Time
	if err := testDB.QueryRowContext(t.Context(), "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		t.Fatal(err)
	}
	insert := func(team entity.Team, assignee *int64, status entity.Status, daysAgo int) entity.Task {
		closed := now.Add(-time.Duration(daysAgo) * 24 * time.Hour)
		task := entity.Task{TeamID: team.ID, Title: "Report fixture", Status: status, CreatedBy: team.CreatedBy,
			AssigneeID: assignee, CreatedAt: closed.Add(-2 * time.Hour), UpdatedAt: closed, Version: 1}
		if status == entity.StatusDone {
			task.ClosedAt = &closed
		}
		if err := f.store.Within(t.Context(), func(tx usecase.Transaction) error { return tx.Tasks().Create(t.Context(), &task) }); err != nil {
			t.Fatal(err)
		}
		return task
	}
	for _, p := range []struct {
		id    int64
		count int
	}{{alice.ID, 3}, {bob.ID, 2}, {carol.ID, 2}, {dan.ID, 1}} {
		for range p.count {
			insert(f.team, &p.id, entity.StatusDone, 1)
		}
	}
	insert(f.team, &alice.ID, entity.StatusDone, 31) // Excluded from 30-day leaders, included in average/count.
	insert(f.team, nil, entity.StatusDone, 29)       // Unassigned tasks count, but cannot be a leader.
	first := insert(f.team, nil, entity.StatusTodo, 1)
	insert(f.team, nil, entity.StatusTodo, 1)
	insert(f.team, nil, entity.StatusInProgress, 1)
	foreign := insert(other.team, &other.owner.ID, entity.StatusDone, 1)
	for _, sample := range []struct {
		task, user int64
		count      int
	}{{first.ID, f.owner.ID, 3}, {foreign.ID, other.owner.ID, 5}} {
		for range sample.count {
			c := entity.Comment{TaskID: sample.task, UserID: sample.user, Content: "A comment", CreatedAt: now}
			if err := f.store.Tasks().AddComment(t.Context(), &c); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, actor := range []int64{f.owner.ID, alice.ID} {
		stats, err := f.teams.Stats(t.Context(), actor, f.team.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stats.TeamID != f.team.ID || stats.TasksByStatus[entity.StatusTodo] != 2 || stats.TasksByStatus[entity.StatusInProgress] != 1 || stats.TasksByStatus[entity.StatusDone] != 10 {
			t.Fatalf("wrong task counts: %+v", stats)
		}
		if stats.CommentCount != 3 {
			t.Fatalf("comment fan-out or team leak: %+v", stats)
		}
		if stats.AverageCloseSeconds == nil || math.Abs(*stats.AverageCloseSeconds-7200) > .001 {
			t.Fatalf("wrong closure average: %+v", stats)
		}
		want := []entity.Performer{{UserID: alice.ID, Name: "Alice", ClosedTasks: 3}, {UserID: bob.ID, Name: "Bob", ClosedTasks: 2}, {UserID: carol.ID, Name: "Carol", ClosedTasks: 2}}
		if len(stats.TopAssignees) != len(want) {
			t.Fatalf("wrong leaders: %+v", stats.TopAssignees)
		}
		for i := range want {
			if stats.TopAssignees[i] != want[i] {
				t.Fatalf("wrong leader %d: %+v", i, stats.TopAssignees)
			}
		}
	}
	if _, err := f.teams.Stats(t.Context(), bob.ID, f.team.ID); !errors.Is(err, entity.ErrForbidden) {
		t.Fatalf("member accessed stats: %v", err)
	}
	if _, err := f.teams.Stats(t.Context(), other.owner.ID, f.team.ID); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("outsider accessed stats: %v", err)
	}
	empty := newFixture(t)
	stats, err := empty.teams.Stats(t.Context(), empty.owner.ID, empty.team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.CommentCount != 0 || stats.AverageCloseSeconds != nil || len(stats.TopAssignees) != 0 || len(stats.TasksByStatus) != 3 {
		t.Fatalf("invalid empty report: %+v", stats)
	}
}
