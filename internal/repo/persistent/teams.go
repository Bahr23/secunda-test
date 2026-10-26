package persistent

import (
	"context"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

type Teams struct{ q querier }

func (r *Teams) Membership(ctx context.Context, teamID, userID int64) (entity.Membership, error) {
	return readMembership(ctx, r.q, teamID, userID)
}

func (r *Teams) List(ctx context.Context, actor int64, page entity.Pagination) ([]entity.Team, error) {
	suffix, args := pageSQL(page, "t.id", false)
	args = append([]any{actor}, args...)
	rows, err := r.q.QueryContext(ctx, `SELECT t.id, t.name, t.created_by, t.created_at, m.role FROM teams t JOIN team_members m ON m.team_id = t.id WHERE m.user_id = ?`+suffix, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]entity.Team, 0)
	for rows.Next() {
		var t entity.Team
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedBy, &t.CreatedAt, &t.Role); err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

var _ usecase.TeamRepository = (*Teams)(nil)

func readMembership(ctx context.Context, q querier, teamID, userID int64) (entity.Membership, error) {
	m := entity.Membership{TeamID: teamID, UserID: userID}
	err := q.QueryRowContext(ctx, `SELECT role FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, userID).Scan(&m.Role)
	return m, mapError(err)
}
