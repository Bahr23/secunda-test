//go:build integration

package integration_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

func TestConcurrentUpdatePreservesTaskAndHistory(t *testing.T) {
	f := newFixture(t)
	task := f.task(t, nil)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, title := range []string{"Winner A", "Winner B"} {
		wg.Go(func() {
			<-start
			_, err := f.tasks.Update(t.Context(), f.owner.ID, task.ID, usecase.UpdateTask{Version: task.Version, Title: &title})
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, entity.ErrConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	current, err := f.store.Tasks().ByID(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	history, err := f.tasks.History(t.Context(), f.owner.ID, task.ID, entity.Pagination{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 2 || len(history.Items) != 2 {
		t.Fatalf("version=%d history=%d", current.Version, len(history.Items))
	}
	change := history.Items[1].Changes["title"]
	if change.Old != "Initial" || change.New != current.Title {
		t.Fatalf("history does not match committed state: %+v", change)
	}
}

var errInjected = errors.New("injected history failure")

type failingTasks struct{ usecase.TaskWriter }

func (f failingTasks) Create(ctx context.Context, task *entity.Task) error {
	if err := f.TaskWriter.Create(ctx, task); err != nil {
		return err
	}
	return errInjected
}
func (f failingTasks) Update(ctx context.Context, previous, task entity.Task, actor int64) error {
	if err := f.TaskWriter.Update(ctx, previous, task, actor); err != nil {
		return err
	}
	return errInjected
}

type failingTx struct{ usecase.Transaction }

func (f failingTx) Tasks() usecase.TaskWriter { return failingTasks{f.Transaction.Tasks()} }

type failingTransactor struct{ usecase.Transactor }

func (f failingTransactor) Within(ctx context.Context, fn func(usecase.Transaction) error) error {
	return f.Transactor.Within(ctx, func(tx usecase.Transaction) error { return fn(failingTx{tx}) })
}

func TestTaskAndHistoryRollbackTogether(t *testing.T) {
	f := newFixture(t)
	task := f.task(t, nil)
	service := usecase.NewTask(f.store.Tasks(), f.store.Teams(), failingTransactor{f.store}, nil)
	_, err := service.Update(t.Context(), f.owner.ID, task.ID, usecase.UpdateTask{Version: 1, Title: pointer("Must roll back")})
	if !errors.Is(err, errInjected) {
		t.Fatalf("unexpected error: %v", err)
	}
	current, err := f.store.Tasks().ByID(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 1 || current.Title != "Initial" {
		t.Fatalf("update escaped rollback: %+v", current)
	}
	history, err := f.tasks.History(t.Context(), f.owner.ID, task.ID, entity.Pagination{Limit: 20})
	if err != nil || len(history.Items) != 1 {
		t.Fatalf("history escaped rollback: %d, %v", len(history.Items), err)
	}
	_, err = service.Create(t.Context(), f.owner.ID, usecase.CreateTask{TeamID: f.team.ID, Title: "Must not exist"})
	if !errors.Is(err, errInjected) {
		t.Fatal(err)
	}
	page, err := f.tasks.List(t.Context(), f.owner.ID, entity.TaskFilter{TeamID: f.team.ID, Pagination: entity.Pagination{Limit: 20}})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("creation escaped rollback: %+v %v", page, err)
	}
	m, err := f.store.Tasks().ListAccess(t.Context(), f.team.ID, f.owner.ID)
	if err != nil || m.Revision != 1 {
		t.Fatalf("revision changed on rollback: %+v %v", m, err)
	}
}

type failingTeams struct{ usecase.TeamWriter }

func (f failingTeams) Create(ctx context.Context, team *entity.Team) error {
	if err := f.TeamWriter.Create(ctx, team); err != nil {
		return err
	}
	return errInjected
}

type failingTeamTx struct{ usecase.Transaction }

func (f failingTeamTx) Teams() usecase.TeamWriter {
	return failingTeams{f.Transaction.Teams()}
}

type failingTeamTransactor struct{ usecase.Transactor }

func (f failingTeamTransactor) Within(ctx context.Context, fn func(usecase.Transaction) error) error {
	return f.Transactor.Within(ctx, func(tx usecase.Transaction) error { return fn(failingTeamTx{tx}) })
}

func TestTeamAndOwnerMembershipRollbackTogether(t *testing.T) {
	f := newFixture(t)
	service := usecase.NewTeam(f.store.Teams(), failingTeamTransactor{f.store})
	team, err := service.Create(t.Context(), f.owner.ID, "Must roll back")
	if !errors.Is(err, errInjected) {
		t.Fatalf("unexpected error: %v", err)
	}
	var teamCount, memberCount int
	if err := testDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM teams WHERE id = ?`, team.ID).Scan(&teamCount); err != nil {
		t.Fatal(err)
	}
	if err := testDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM team_members WHERE team_id = ?`, team.ID).Scan(&memberCount); err != nil {
		t.Fatal(err)
	}
	if teamCount != 0 || memberCount != 0 {
		t.Fatalf("teamCount=%d memberCount=%d", teamCount, memberCount)
	}
}

func TestInviteUsesOneConnectionAndPreservesExistingRole(t *testing.T) {
	f := newFixture(t)
	user := newUser(t, f.store, "New member")
	testDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		testDB.SetMaxOpenConns(20)
		testDB.SetMaxIdleConns(10)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := f.teams.Invite(ctx, f.owner.ID, f.team.ID, user.ID, entity.RoleMember); err != nil {
		t.Fatalf("invitation must work with a single connection: %v", err)
	}
	if err := f.teams.Invite(ctx, f.owner.ID, f.team.ID, user.ID, entity.RoleAdmin); !errors.Is(err, entity.ErrConflict) {
		t.Fatalf("duplicate invitation must not escalate a role: %v", err)
	}
	member, err := f.store.Teams().Membership(ctx, f.team.ID, user.ID)
	if err != nil || member.Role != entity.RoleMember {
		t.Fatalf("existing role changed: %+v %v", member, err)
	}
	if err := f.teams.Invite(ctx, f.owner.ID, f.team.ID, 9223372036854775807, entity.RoleMember); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("nonexistent invitee: %v", err)
	}
}

func TestTaskWriteRollsBackWhenHistoryFails(t *testing.T) {
	f := newFixture(t)
	task := f.task(t, nil)
	err := f.store.Within(t.Context(), func(tx usecase.Transaction) error {
		previous, err := tx.Tasks().ForUpdate(t.Context(), task.ID)
		if err != nil {
			return err
		}
		updated := previous
		updated.Title = "Must roll back with history"
		updated.Version++
		// The task update succeeds; the following audit insert violates its author FK.
		return tx.Tasks().Update(t.Context(), previous, updated, 9223372036854775807)
	})
	if !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("expected audit foreign key failure: %v", err)
	}
	current, err := f.store.Tasks().ByID(t.Context(), task.ID)
	if err != nil || current.Title != task.Title || current.Version != 1 {
		t.Fatalf("task survived failed history: %+v %v", current, err)
	}
	history, err := f.tasks.History(t.Context(), f.owner.ID, task.ID, entity.Pagination{Limit: 20})
	if err != nil || len(history.Items) != 1 {
		t.Fatalf("unexpected history after rollback: %+v %v", history, err)
	}
	access, err := f.store.Tasks().ListAccess(t.Context(), f.team.ID, f.owner.ID)
	if err != nil || access.Revision != 1 {
		t.Fatalf("revision changed after rollback: %+v %v", access, err)
	}
}
