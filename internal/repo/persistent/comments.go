package persistent

import (
	"context"

	"github.com/bahr/secunda/internal/entity"
)

func (r *Tasks) AddComment(ctx context.Context, c *entity.Comment) error {
	result, err := r.q.ExecContext(ctx, `INSERT INTO task_comments (task_id, user_id, content, created_at) VALUES (?, ?, ?, ?)`, c.TaskID, c.UserID, c.Content, c.CreatedAt)
	if err != nil {
		return mapError(err)
	}
	c.ID, err = result.LastInsertId()
	return err
}

func (r *Tasks) Comments(ctx context.Context, taskID int64, page entity.Pagination) ([]entity.Comment, error) {
	suffix, args := pageSQL(page, "id", true)
	args = append([]any{taskID}, args...)
	rows, err := r.q.QueryContext(ctx, `SELECT id, task_id, user_id, content, created_at FROM task_comments WHERE task_id = ?`+suffix, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]entity.Comment, 0)
	for rows.Next() {
		var c entity.Comment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.UserID, &c.Content, &c.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}
