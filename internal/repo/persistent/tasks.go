package persistent

import (
	"context"
	"strings"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

type Tasks struct{ q querier }

const taskColumns = `id, team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version`

type scanner interface{ Scan(...any) error }

func scanTask(row scanner) (entity.Task, error) {
	var t entity.Task
	err := row.Scan(&t.ID, &t.TeamID, &t.Title, &t.Description, &t.Status, &t.CreatedBy, &t.AssigneeID, &t.CreatedAt, &t.UpdatedAt, &t.ClosedAt, &t.Version)
	return t, mapError(err)
}

func (r *Tasks) ByID(ctx context.Context, id int64) (entity.Task, error) {
	return scanTask(r.q.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id))
}

func (r *Tasks) List(ctx context.Context, f entity.TaskFilter) ([]entity.Task, error) {
	var query strings.Builder
	query.WriteString(`SELECT ` + taskColumns + ` FROM tasks WHERE team_id = ?`)
	args := []any{f.TeamID}
	if f.Status != "" {
		query.WriteString(` AND status = ?`)
		args = append(args, f.Status)
	}
	if f.AssigneeID != nil {
		query.WriteString(` AND assignee_id = ?`)
		args = append(args, *f.AssigneeID)
	}
	suffix, pageArgs := pageSQL(f.Pagination, "id", false)
	query.WriteString(suffix)
	args = append(args, pageArgs...)
	rows, err := r.q.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]entity.Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

var _ usecase.TaskRepository = (*Tasks)(nil)

func (r *Tasks) ListAccess(ctx context.Context, teamID, userID int64) (usecase.TaskListAccess, error) {
	access := usecase.TaskListAccess{Membership: entity.Membership{TeamID: teamID, UserID: userID}}
	err := r.q.QueryRowContext(ctx, `SELECT m.role, t.task_revision FROM team_members m JOIN teams t ON t.id = m.team_id WHERE m.team_id = ? AND m.user_id = ?`, teamID, userID).Scan(&access.Membership.Role, &access.Revision)
	return access, mapError(err)
}
