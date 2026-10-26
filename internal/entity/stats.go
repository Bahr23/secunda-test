package entity

type Performer struct {
	UserID      int64  `json:"user_id"`
	Name        string `json:"name"`
	ClosedTasks int64  `json:"closed_tasks"`
}

type TeamStats struct {
	TeamID              int64            `json:"team_id"`
	TasksByStatus       map[Status]int64 `json:"tasks_by_status"`
	TopAssignees        []Performer      `json:"top_assignees"`
	AverageCloseSeconds *float64         `json:"average_close_seconds"`
	CommentCount        int64            `json:"comment_count"`
}
