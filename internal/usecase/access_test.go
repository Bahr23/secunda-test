package usecase_test

import (
	"errors"
	"testing"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

func TestTaskMembershipGatesCreationHistoryAndComments(t *testing.T) {
	for _, role := range []entity.Role{entity.RoleOwner, entity.RoleAdmin, entity.RoleMember, ""} {
		t.Run(string(role), func(t *testing.T) {
			teams := &teamAdapter{roles: map[int64]entity.Role{}, onlyTeam: 2}
			if role != "" {
				teams.roles[10] = role
			}
			tasks := &taskAdapter{current: entity.Task{ID: 1, TeamID: 2, CreatedBy: 10}}
			service := usecase.NewTask(tasks, teams, transactionAdapter{teams, tasks}, nil)
			want := error(nil)
			if role == "" {
				want = entity.ErrNotFound
			}
			_, err := service.Create(t.Context(), 10, usecase.CreateTask{TeamID: 2, Title: "Task"})
			if !errors.Is(err, want) {
				t.Fatalf("create: %v", err)
			}
			_, err = service.History(t.Context(), 10, 1, entity.Pagination{Limit: 20})
			if !errors.Is(err, want) {
				t.Fatalf("history: %v", err)
			}
			_, err = service.Comments(t.Context(), 10, 1, entity.Pagination{Limit: 20})
			if !errors.Is(err, want) {
				t.Fatalf("comments: %v", err)
			}
			_, err = service.Comment(t.Context(), 10, 1, "A comment")
			if !errors.Is(err, want) {
				t.Fatalf("comment: %v", err)
			}
			count := 1
			if want != nil {
				count = 0
			}
			if tasks.created != count || tasks.historyReads != count || tasks.commentReads != count || tasks.commentWrites != count {
				t.Fatalf("access checks did not gate persistence: %+v", tasks)
			}
		})
	}
}

func TestMembershipInAnotherTeamDoesNotGrantTaskAccess(t *testing.T) {
	teams := &teamAdapter{roles: map[int64]entity.Role{10: entity.RoleOwner}, onlyTeam: 99}
	tasks := &taskAdapter{current: entity.Task{ID: 1, TeamID: 2, CreatedBy: 10, Version: 1}}
	service := usecase.NewTask(tasks, teams, transactionAdapter{teams, tasks}, nil)
	_, err := service.Update(t.Context(), 10, 1, usecase.UpdateTask{Version: 1, Title: ptr("Denied")})
	if !errors.Is(err, entity.ErrNotFound) || tasks.writes != 0 {
		t.Fatalf("another team's owner edited task: %v", err)
	}
	_, err = service.Comment(t.Context(), 10, 1, "Denied")
	if !errors.Is(err, entity.ErrNotFound) || tasks.commentWrites != 0 {
		t.Fatalf("another team's owner commented: %v", err)
	}
	_, err = service.Create(t.Context(), 10, usecase.CreateTask{TeamID: 2, Title: "Denied"})
	if !errors.Is(err, entity.ErrNotFound) || tasks.created != 0 {
		t.Fatalf("another team's owner created task: %v", err)
	}
}

func TestReportAccessDoesNotReachRepositoryForMembersOrOutsiders(t *testing.T) {
	for _, role := range []entity.Role{entity.RoleOwner, entity.RoleAdmin, entity.RoleMember, ""} {
		t.Run(string(role), func(t *testing.T) {
			teams := &teamAdapter{roles: map[int64]entity.Role{}}
			if role != "" {
				teams.roles[1] = role
			}
			service := usecase.NewTeam(teams, transactionAdapter{teams: teams})
			_, err := service.Stats(t.Context(), 1, 2)
			want := error(nil)
			if role == entity.RoleMember {
				want = entity.ErrForbidden
			}
			if role == "" {
				want = entity.ErrNotFound
			}
			if !errors.Is(err, want) || (teams.statsReads == 1) != (want == nil) {
				t.Fatalf("role=%s error=%v reads=%d", role, err, teams.statsReads)
			}
		})
	}
}

func TestTeamRoleChangesAndInvitationsRejectMissingMembership(t *testing.T) {
	for _, roles := range []map[int64]entity.Role{{3: entity.RoleMember}, {1: entity.RoleOwner}} {
		teams := &teamAdapter{roles: roles}
		service := usecase.NewTeam(teams, transactionAdapter{teams: teams})
		if err := service.ChangeRole(t.Context(), 1, 2, 3, entity.RoleAdmin); !errors.Is(err, entity.ErrNotFound) || teams.changed != 0 {
			t.Fatalf("missing membership changed role: %v", err)
		}
	}
	teams := &teamAdapter{roles: map[int64]entity.Role{}}
	service := usecase.NewTeam(teams, transactionAdapter{teams: teams})
	if err := service.Invite(t.Context(), 1, 2, 3, entity.RoleMember); !errors.Is(err, entity.ErrNotFound) || teams.added != 0 {
		t.Fatalf("outsider invited member: %v", err)
	}
}
