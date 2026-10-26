package persistent

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

// A taskWriter can only be constructed with an active SQL transaction.
type taskWriter struct{ q *sql.Tx }

func (r *taskWriter) ForUpdate(ctx context.Context, id int64) (entity.Task, error) {
	return scanTask(r.q.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ? FOR UPDATE`, id))
}

func (r *taskWriter) Create(ctx context.Context, t *entity.Task) error {
	result, err := r.q.ExecContext(ctx, `INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, t.TeamID, t.Title, t.Description, t.Status, t.CreatedBy, t.AssigneeID, t.CreatedAt, t.UpdatedAt, t.ClosedAt, t.Version)
	if err != nil {
		return fmt.Errorf("insert task: %w", mapError(err))
	}
	t.ID, err = result.LastInsertId()
	if err != nil {
		return err
	}
	return r.recordChange(ctx, *t, nil, t.CreatedBy)
}

func (r *taskWriter) Update(ctx context.Context, previous, t entity.Task, actor int64) error {
	result, err := r.q.ExecContext(ctx, `UPDATE tasks SET title = ?, description = ?, status = ?, assignee_id = ?, updated_at = ?, closed_at = ?, version = ? WHERE id = ? AND version = ?`, t.Title, t.Description, t.Status, t.AssigneeID, t.UpdatedAt, t.ClosedAt, t.Version, t.ID, previous.Version)
	if err != nil {
		return fmt.Errorf("update task: %w", mapError(err))
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return entity.ErrConflict
	}
	return r.recordChange(ctx, t, &previous, actor)
}

// recordChange is the single persistence path for a task's audit and list revision.
func (r *taskWriter) recordChange(ctx context.Context, task entity.Task, previous *entity.Task, actor int64) error {
	history := entity.History{TaskID: task.ID, ChangedBy: actor, Changes: task.ChangesFrom(previous), CreatedAt: task.UpdatedAt}
	if err := insertHistory(ctx, r.q, history); err != nil {
		return fmt.Errorf("save task history: %w", err)
	}
	_, err := r.q.ExecContext(ctx, `UPDATE teams SET task_revision = task_revision + 1 WHERE id = ?`, task.TeamID)
	if err != nil {
		return fmt.Errorf("advance task list revision: %w", mapError(err))
	}
	return nil
}

var _ usecase.TaskWriter = (*taskWriter)(nil)
