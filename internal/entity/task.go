package entity

import (
	"reflect"
	"time"
)

type Status string

const (
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

func (s Status) Valid() bool {
	return s == StatusTodo || s == StatusInProgress || s == StatusDone
}

type Task struct {
	ID          int64      `json:"id"`
	TeamID      int64      `json:"team_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      Status     `json:"status"`
	CreatedBy   int64      `json:"created_by"`
	AssigneeID  *int64     `json:"assignee_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	Version     int64      `json:"version"`
}

type TaskFilter struct {
	TeamID     int64
	Status     Status
	AssigneeID *int64
	Pagination
}

type Change struct {
	Old any `json:"old"`
	New any `json:"new"`
}

type History struct {
	ID        int64             `json:"id"`
	TaskID    int64             `json:"task_id"`
	ChangedBy int64             `json:"changed_by"`
	Changes   map[string]Change `json:"changes"`
	CreatedAt time.Time         `json:"created_at"`
}

// ChangesFrom captures persisted fields only; nil denotes task creation.
func (t Task) ChangesFrom(previous *Task) map[string]Change {
	fields := func(t Task) map[string]any {
		return map[string]any{
			"team_id": t.TeamID, "title": t.Title, "description": t.Description,
			"status": t.Status, "created_by": t.CreatedBy, "assignee_id": t.AssigneeID,
			"created_at": t.CreatedAt, "updated_at": t.UpdatedAt,
			"closed_at": t.ClosedAt, "version": t.Version,
		}
	}
	old := map[string]any{}
	if previous != nil {
		old = fields(*previous)
	}
	changes := make(map[string]Change)
	for key, value := range fields(t) {
		if previous == nil || !reflect.DeepEqual(old[key], value) {
			changes[key] = Change{Old: old[key], New: value}
		}
	}
	return changes
}

type Comment struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}
