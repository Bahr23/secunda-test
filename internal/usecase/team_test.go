package usecase_test

import (
	"errors"
	"testing"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

func TestInvitationPermissions(t *testing.T) {
	for _, tc := range []struct {
		name           string
		actor, invited entity.Role
		want           error
	}{
		{"owner invites member", entity.RoleOwner, entity.RoleMember, nil},
		{"owner invites admin", entity.RoleOwner, entity.RoleAdmin, nil},
		{"admin invites member", entity.RoleAdmin, entity.RoleMember, nil},
		{"default role is member", entity.RoleAdmin, "", nil},
		{"admin cannot grant admin", entity.RoleAdmin, entity.RoleAdmin, entity.ErrForbidden},
		{"member cannot invite", entity.RoleMember, entity.RoleMember, entity.ErrForbidden},
		{"owner cannot grant owner", entity.RoleOwner, entity.RoleOwner, entity.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			teams := &teamAdapter{roles: map[int64]entity.Role{1: tc.actor}}
			service := usecase.NewTeam(teams, transactionAdapter{teams: teams})
			err := service.Invite(t.Context(), 1, 2, 3, tc.invited)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if (teams.added == 1) != (tc.want == nil) {
				t.Fatal("unexpected membership write")
			}
		})
	}
}

func TestRoleChangesProtectOwner(t *testing.T) {
	for _, tc := range []struct {
		name          string
		actor, target entity.Role
		allowed       bool
	}{
		{"owner changes member", entity.RoleOwner, entity.RoleMember, true},
		{"owner cannot change owner", entity.RoleOwner, entity.RoleOwner, false},
		{"admin cannot change roles", entity.RoleAdmin, entity.RoleMember, false},
		{"member cannot change roles", entity.RoleMember, entity.RoleMember, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			teams := &teamAdapter{roles: map[int64]entity.Role{1: tc.actor, 3: tc.target}}
			service := usecase.NewTeam(teams, transactionAdapter{teams: teams})
			err := service.ChangeRole(t.Context(), 1, 2, 3, entity.RoleAdmin)
			if (err == nil) != tc.allowed || (teams.changed == 1) != tc.allowed {
				t.Fatalf("writes=%d error=%v", teams.changed, err)
			}
		})
	}
}
