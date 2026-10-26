package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bahr/secunda/internal/entity"
)

type Team struct {
	teams TeamRepository
	tx    Transactor
}

func NewTeam(teams TeamRepository, tx Transactor) *Team {
	return &Team{teams: teams, tx: tx}
}

func membership(ctx context.Context, teams MembershipReader, teamID, userID int64) (entity.Membership, error) {
	m, err := teams.Membership(ctx, teamID, userID)
	if errors.Is(err, entity.ErrNotFound) {
		return m, entity.ErrNotFound
	}
	if err != nil {
		return m, fmt.Errorf("check team membership: %w", err)
	}
	return m, nil
}

func (s *Team) Create(ctx context.Context, actor int64, name string) (entity.Team, error) {
	name = strings.TrimSpace(name)
	if err := validateText(name, "name", 100, true); err != nil {
		return entity.Team{}, err
	}
	t := entity.Team{Name: name, CreatedBy: actor, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Role: entity.RoleOwner}
	err := s.tx.Within(ctx, func(tx Transaction) error {
		return tx.Teams().Create(ctx, &t)
	})
	return t, err
}

func (s *Team) List(ctx context.Context, actor int64, page entity.Pagination) (entity.TeamPage, error) {
	if !page.Valid() {
		return entity.TeamPage{}, entity.ErrInvalid
	}
	items, err := s.teams.List(ctx, actor, page)
	return entity.NewPage(items, page, func(t entity.Team) int64 { return t.ID }), err
}

func (s *Team) Invite(ctx context.Context, actor, teamID, userID int64, role entity.Role) error {
	if userID <= 0 || teamID <= 0 {
		return entity.ErrInvalid
	}
	if role == "" {
		role = entity.RoleMember
	}
	if role != entity.RoleMember && role != entity.RoleAdmin {
		return entity.ErrInvalid
	}
	return s.tx.Within(ctx, func(tx Transaction) error {
		m, err := membership(ctx, tx.Teams(), teamID, actor)
		if err != nil {
			return err
		}
		if !m.Role.CanManage() || (role == entity.RoleAdmin && m.Role != entity.RoleOwner) {
			return entity.ErrForbidden
		}
		// The repository's foreign key verifies the user inside this transaction.
		return tx.Teams().AddMember(ctx, entity.Membership{TeamID: teamID, UserID: userID, Role: role})
	})
}

func (s *Team) ChangeRole(ctx context.Context, actor, teamID, userID int64, role entity.Role) error {
	if role != entity.RoleAdmin && role != entity.RoleMember {
		return entity.ErrInvalid
	}
	return s.tx.Within(ctx, func(tx Transaction) error {
		m, err := membership(ctx, tx.Teams(), teamID, actor)
		if err != nil {
			return err
		}
		if m.Role != entity.RoleOwner {
			return entity.ErrForbidden
		}
		target, err := membership(ctx, tx.Teams(), teamID, userID)
		if err != nil {
			return err
		}
		if target.Role == entity.RoleOwner {
			return entity.ErrForbidden
		}
		target.Role = role
		return tx.Teams().ChangeRole(ctx, target)
	})
}

func (s *Team) Stats(ctx context.Context, actor, teamID int64) (entity.TeamStats, error) {
	m, err := membership(ctx, s.teams, teamID, actor)
	if err != nil {
		return entity.TeamStats{}, err
	}
	if !m.Role.CanManage() {
		return entity.TeamStats{}, entity.ErrForbidden
	}
	return s.teams.Stats(ctx, teamID)
}
