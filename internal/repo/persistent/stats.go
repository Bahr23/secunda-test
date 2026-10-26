package persistent

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"sort"

	"github.com/bahr/secunda/internal/entity"
)

//go:embed stats.sql
var statsQuery string

func (r *Teams) Stats(ctx context.Context, teamID int64) (entity.TeamStats, error) {
	stats := entity.TeamStats{TeamID: teamID,
		TasksByStatus: map[entity.Status]int64{entity.StatusTodo: 0, entity.StatusInProgress: 0, entity.StatusDone: 0},
		TopAssignees:  []entity.Performer{}}
	rows, err := r.q.QueryContext(ctx, statsQuery, teamID)
	if err != nil {
		return stats, fmt.Errorf("query team stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var kind string
		var label sql.NullString
		var userID sql.NullInt64
		var amount int64
		var average sql.NullFloat64
		if err := rows.Scan(&kind, &label, &userID, &amount, &average); err != nil {
			return stats, err
		}
		switch kind {
		case "status":
			stats.TasksByStatus[entity.Status(label.String)] = amount
		case "assignee":
			stats.TopAssignees = append(stats.TopAssignees, entity.Performer{UserID: userID.Int64, Name: label.String, ClosedTasks: amount})
		case "summary":
			stats.CommentCount = amount
			if average.Valid {
				stats.AverageCloseSeconds = &average.Float64
			}
		}
	}
	// UNION ALL does not guarantee the order of its constituent queries.
	sort.Slice(stats.TopAssignees, func(i, j int) bool {
		a, b := stats.TopAssignees[i], stats.TopAssignees[j]
		if a.ClosedTasks != b.ClosedTasks {
			return a.ClosedTasks > b.ClosedTasks
		}
		return a.UserID < b.UserID
	})
	return stats, rows.Err()
}
