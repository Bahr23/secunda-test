package usecase_test

import (
	"context"
	"fmt"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

func ptr[T any](v T) *T { return &v }

// These adapters implement the complete test seam explicitly. They record the
// commands sent by usecases; transaction semantics are tested against MySQL.
type teamAdapter struct {
	roles      map[int64]entity.Role
	added      int
	changed    int
	statsReads int
	onlyTeam   int64
}

func (a *teamAdapter) Membership(_ context.Context, team, user int64) (entity.Membership, error) {
	if a.onlyTeam != 0 && a.onlyTeam != team {
		return entity.Membership{}, entity.ErrNotFound
	}
	role, ok := a.roles[user]
	if !ok {
		return entity.Membership{}, entity.ErrNotFound
	}
	return entity.Membership{TeamID: team, UserID: user, Role: role}, nil
}
func (a *teamAdapter) Create(context.Context, *entity.Team) error {
	return fmt.Errorf("unexpected team creation")
}
func (a *teamAdapter) AddMember(_ context.Context, m entity.Membership) error {
	if _, exists := a.roles[m.UserID]; exists {
		return entity.ErrConflict
	}
	a.roles[m.UserID] = m.Role
	a.added++
	return nil
}
func (a *teamAdapter) ChangeRole(_ context.Context, m entity.Membership) error {
	a.roles[m.UserID] = m.Role
	a.changed++
	return nil
}
func (a *teamAdapter) List(context.Context, int64, entity.Pagination) ([]entity.Team, error) {
	return nil, fmt.Errorf("unexpected team listing")
}
func (a *teamAdapter) Stats(_ context.Context, team int64) (entity.TeamStats, error) {
	a.statsReads++
	return entity.TeamStats{TeamID: team}, nil
}

type taskAdapter struct {
	current       entity.Task
	accessErr     error
	writes        int
	reads         int
	previous      entity.Task
	actor         int64
	writeErr      error
	created       int
	historyReads  int
	commentReads  int
	commentWrites int
}

func (a *taskAdapter) ByID(_ context.Context, id int64) (entity.Task, error) {
	if id != a.current.ID {
		return entity.Task{}, entity.ErrNotFound
	}
	return a.current, nil
}
func (a *taskAdapter) ForUpdate(ctx context.Context, id int64) (entity.Task, error) {
	return a.ByID(ctx, id)
}
func (a *taskAdapter) Create(_ context.Context, task *entity.Task) error {
	a.created++
	task.ID = 1
	a.current = *task
	return nil
}
func (a *taskAdapter) Update(_ context.Context, previous, updated entity.Task, actor int64) error {
	if a.writeErr != nil {
		return a.writeErr
	}
	a.previous, a.current, a.actor = previous, updated, actor
	a.writes++
	return nil
}
func (a *taskAdapter) ListAccess(_ context.Context, team, user int64) (usecase.TaskListAccess, error) {
	return usecase.TaskListAccess{Membership: entity.Membership{TeamID: team, UserID: user, Role: entity.RoleMember}, Revision: 7}, a.accessErr
}
func (a *taskAdapter) List(context.Context, entity.TaskFilter) ([]entity.Task, error) {
	a.reads++
	return []entity.Task{a.current}, nil
}
func (a *taskAdapter) History(context.Context, int64, entity.Pagination) ([]entity.History, error) {
	a.historyReads++
	return []entity.History{}, nil
}
func (a *taskAdapter) AddComment(context.Context, *entity.Comment) error {
	a.commentWrites++
	return nil
}
func (a *taskAdapter) Comments(context.Context, int64, entity.Pagination) ([]entity.Comment, error) {
	a.commentReads++
	return []entity.Comment{}, nil
}

type transactionAdapter struct {
	teams *teamAdapter
	tasks *taskAdapter
}

func (a transactionAdapter) Teams() usecase.TeamWriter { return a.teams }
func (a transactionAdapter) Tasks() usecase.TaskWriter { return a.tasks }
func (a transactionAdapter) Within(_ context.Context, fn func(usecase.Transaction) error) error {
	return fn(a)
}

type cacheAdapter struct {
	called   bool
	hit      bool
	revision int64
	page     entity.TaskPage
}

func (a *cacheAdapter) Get(_ context.Context, _ entity.TaskFilter, revision int64) (entity.TaskPage, bool) {
	a.called = true
	a.revision = revision
	return a.page, a.hit
}
func (a *cacheAdapter) Set(_ context.Context, _ entity.TaskFilter, revision int64, page entity.TaskPage) {
	a.called = true
	a.revision = revision
	a.page = page
}
