package persistent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/bahr/secunda/internal/entity"
)

func insertHistory(ctx context.Context, q querier, h entity.History) error {
	changes, err := json.Marshal(h.Changes)
	if err != nil {
		return fmt.Errorf("encode history: %w", err)
	}
	_, err = q.ExecContext(ctx, `INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES (?, ?, ?, ?)`, h.TaskID, h.ChangedBy, changes, h.CreatedAt)
	return mapError(err)
}

func (r *Tasks) History(ctx context.Context, taskID int64, page entity.Pagination) ([]entity.History, error) {
	suffix, args := pageSQL(page, "id", true)
	args = append([]any{taskID}, args...)
	rows, err := r.q.QueryContext(ctx, `SELECT id, task_id, changed_by, changes, created_at FROM task_history WHERE task_id = ?`+suffix, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]entity.History, 0)
	for rows.Next() {
		var h entity.History
		var changes []byte
		if err := rows.Scan(&h.ID, &h.TaskID, &h.ChangedBy, &changes, &h.CreatedAt); err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(changes))
		// Changes hold heterogeneous JSON values; retain exact int64 IDs and versions.
		decoder.UseNumber()
		if err := decoder.Decode(&h.Changes); err != nil {
			return nil, fmt.Errorf("decode history: %w", err)
		}
		items = append(items, h)
	}
	return items, rows.Err()
}
