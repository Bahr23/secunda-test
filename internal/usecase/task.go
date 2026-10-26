package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bahr/secunda/internal/entity"
)

type Task struct {
	tasks TaskRepository
	teams MembershipReader
	tx    Transactor
	cache TaskCache
}

func NewTask(tasks TaskRepository, teams MembershipReader, tx Transactor, cache TaskCache) *Task {
	return &Task{tasks: tasks, teams: teams, tx: tx, cache: cache}
}

type CreateTask struct {
	TeamID      int64
	Title       string
	Description string
	AssigneeID  *int64
}

type UpdateTask struct {
	Version     int64
	Title       *string
	Description *string
	Status      *entity.Status
	AssigneeID  *int64
	SetAssignee bool
}

func (in UpdateTask) validate() error {
	if in.Version < 1 {
		return fmt.Errorf("%w: version is required", entity.ErrInvalid)
	}
	if in.Title == nil && in.Description == nil && in.Status == nil && !in.SetAssignee {
		return fmt.Errorf("%w: at least one changed field is required", entity.ErrInvalid)
	}
	if in.Title != nil {
		if err := validateText(*in.Title, "title", 200, true); err != nil {
			return err
		}
	}
	if in.Description != nil {
		if err := validateText(*in.Description, "description", 10000, false); err != nil {
			return err
		}
	}
	if in.Status != nil && !in.Status.Valid() {
		return fmt.Errorf("%w: unknown status", entity.ErrInvalid)
	}
	return nil
}

func authorizeTaskUpdate(actor int64, role entity.Role, task entity.Task, in UpdateTask) error {
	if role.CanManage() || task.CreatedBy == actor {
		return nil
	}
	if task.AssigneeID != nil && *task.AssigneeID == actor && in.Title == nil && in.Description == nil && !in.SetAssignee {
		return nil
	}
	return entity.ErrForbidden
}

func validateAssignee(ctx context.Context, teams MembershipReader, teamID int64, assignee *int64) error {
	if assignee == nil {
		return nil
	}
	if *assignee <= 0 {
		return fmt.Errorf("%w: invalid assignee_id", entity.ErrInvalid)
	}
	_, err := membership(ctx, teams, teamID, *assignee)
	if err != nil {
		return fmt.Errorf("validate assignee: %w", err)
	}
	return nil
}

func (s *Task) Create(ctx context.Context, actor int64, in CreateTask) (entity.Task, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.TeamID <= 0 {
		return entity.Task{}, entity.ErrInvalid
	}
	if err := validateText(in.Title, "title", 200, true); err != nil {
		return entity.Task{}, err
	}
	if err := validateText(in.Description, "description", 10000, false); err != nil {
		return entity.Task{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	task := entity.Task{TeamID: in.TeamID, Title: in.Title, Description: in.Description,
		Status: entity.StatusTodo, CreatedBy: actor, AssigneeID: in.AssigneeID,
		CreatedAt: now, UpdatedAt: now, Version: 1}
	err := s.tx.Within(ctx, func(tx Transaction) error {
		if _, err := membership(ctx, tx.Teams(), in.TeamID, actor); err != nil {
			return err
		}
		if err := validateAssignee(ctx, tx.Teams(), in.TeamID, in.AssigneeID); err != nil {
			return err
		}
		return tx.Tasks().Create(ctx, &task)
	})
	return task, err
}

func (s *Task) List(ctx context.Context, actor int64, filter entity.TaskFilter) (entity.TaskPage, error) {
	if filter.TeamID <= 0 || !filter.Valid() || (filter.Status != "" && !filter.Status.Valid()) || (filter.AssigneeID != nil && *filter.AssigneeID <= 0) {
		return entity.TaskPage{}, entity.ErrInvalid
	}
	// Authorize before looking in Redis; cache entries are shared by team members.
	access, err := s.tasks.ListAccess(ctx, filter.TeamID, actor)
	if err != nil {
		return entity.TaskPage{}, err
	}
	if s.cache != nil {
		if page, ok := s.cache.Get(ctx, filter, access.Revision); ok {
			page.Pagination = filter.Pagination
			return page, nil
		}
	}
	tasks, err := s.tasks.List(ctx, filter)
	if err != nil {
		return entity.TaskPage{}, err
	}
	page := entity.NewPage(tasks, filter.Pagination, func(t entity.Task) int64 { return t.ID })
	if s.cache != nil {
		s.cache.Set(ctx, filter, access.Revision, page)
	}
	return page, nil
}

func (s *Task) Update(ctx context.Context, actor, taskID int64, in UpdateTask) (entity.Task, error) {
	if err := in.validate(); err != nil {
		return entity.Task{}, err
	}
	var updated entity.Task
	err := s.tx.Within(ctx, func(tx Transaction) error {
		current, err := tx.Tasks().ForUpdate(ctx, taskID)
		if err != nil {
			return err
		}
		m, err := membership(ctx, tx.Teams(), current.TeamID, actor)
		if err != nil {
			return err
		}
		if err := authorizeTaskUpdate(actor, m.Role, current, in); err != nil {
			return err
		}
		if current.Version != in.Version {
			return fmt.Errorf("%w: task version is stale; reload and retry", entity.ErrConflict)
		}
		if in.SetAssignee {
			if err := validateAssignee(ctx, tx.Teams(), current.TeamID, in.AssigneeID); err != nil {
				return err
			}
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		updated = applyTaskUpdate(current, in, now)
		if len(updated.ChangesFrom(&current)) == 0 {
			return nil
		}
		updated.Version++
		updated.UpdatedAt = now
		return tx.Tasks().Update(ctx, current, updated, actor)
	})
	return updated, err
}

func applyTaskUpdate(task entity.Task, in UpdateTask, now time.Time) entity.Task {
	if in.Title != nil {
		task.Title = strings.TrimSpace(*in.Title)
	}
	if in.Description != nil {
		task.Description = *in.Description
	}
	if in.SetAssignee {
		task.AssigneeID = in.AssigneeID
	}
	if in.Status != nil && *in.Status != task.Status {
		task.Status = *in.Status
		task.ClosedAt = nil
		if task.Status == entity.StatusDone {
			task.ClosedAt = &now
		}
	}
	return task
}

func (s *Task) visibleTask(ctx context.Context, actor, taskID int64) (entity.Task, error) {
	task, err := s.tasks.ByID(ctx, taskID)
	if err != nil {
		return task, err
	}
	_, err = membership(ctx, s.teams, task.TeamID, actor)
	return task, err
}

func (s *Task) History(ctx context.Context, actor, taskID int64, page entity.Pagination) (entity.HistoryPage, error) {
	if !page.Valid() {
		return entity.HistoryPage{}, entity.ErrInvalid
	}
	if _, err := s.visibleTask(ctx, actor, taskID); err != nil {
		return entity.HistoryPage{}, err
	}
	items, err := s.tasks.History(ctx, taskID, page)
	return entity.NewPage(items, page, func(item entity.History) int64 { return item.ID }), err
}

func (s *Task) Comment(ctx context.Context, actor, taskID int64, content string) (entity.Comment, error) {
	content = strings.TrimSpace(content)
	if err := validateText(content, "content", 10000, true); err != nil {
		return entity.Comment{}, err
	}
	if _, err := s.visibleTask(ctx, actor, taskID); err != nil {
		return entity.Comment{}, err
	}
	c := entity.Comment{TaskID: taskID, UserID: actor, Content: content, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	err := s.tasks.AddComment(ctx, &c)
	return c, err
}

func (s *Task) Comments(ctx context.Context, actor, taskID int64, page entity.Pagination) (entity.CommentPage, error) {
	if !page.Valid() {
		return entity.CommentPage{}, entity.ErrInvalid
	}
	if _, err := s.visibleTask(ctx, actor, taskID); err != nil {
		return entity.CommentPage{}, err
	}
	items, err := s.tasks.Comments(ctx, taskID, page)
	return entity.NewPage(items, page, func(item entity.Comment) int64 { return item.ID }), err
}
