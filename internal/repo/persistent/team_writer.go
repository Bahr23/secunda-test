package persistent

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

type teamWriter struct{ q *sql.Tx }

func (r *teamWriter) Membership(ctx context.Context, teamID, userID int64) (entity.Membership, error) {
	return readMembership(ctx, r.q, teamID, userID)
}

func (r *teamWriter) Create(ctx context.Context, team *entity.Team) error {
	result, err := r.q.ExecContext(ctx, `INSERT INTO teams (name, created_by, created_at) VALUES (?, ?, ?)`, team.Name, team.CreatedBy, team.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert team: %w", mapError(err))
	}
	team.ID, err = result.LastInsertId()
	if err != nil {
		return err
	}
	return r.AddMember(ctx, entity.Membership{TeamID: team.ID, UserID: team.CreatedBy, Role: entity.RoleOwner})
}

func (r *teamWriter) AddMember(ctx context.Context, m entity.Membership) error {
	_, err := r.q.ExecContext(ctx, `INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)`, m.TeamID, m.UserID, m.Role)
	return mapError(err)
}

func (r *teamWriter) ChangeRole(ctx context.Context, m entity.Membership) error {
	_, err := r.q.ExecContext(ctx, `UPDATE team_members SET role = ? WHERE team_id = ? AND user_id = ? AND role <> 'owner'`, m.Role, m.TeamID, m.UserID)
	return mapError(err)
}

var _ usecase.TeamWriter = (*teamWriter)(nil)
