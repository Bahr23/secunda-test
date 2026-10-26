package usecase_test

import (
	"errors"
	"testing"
	"time"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

func TestTaskUpdatePermissions(t *testing.T) {
	status := usecase.UpdateTask{Status: ptr(entity.StatusDone)}
	full := usecase.UpdateTask{Title: ptr("Changed"), Description: ptr("Text"), SetAssignee: true, AssigneeID: ptr(int64(30))}
	for _, tc := range []struct {
		name   string
		actor  int64
		role   entity.Role
		update usecase.UpdateTask
		want   error
	}{
		{"owner edits all fields", 1, entity.RoleOwner, full, nil},
		{"admin edits all fields", 2, entity.RoleAdmin, full, nil},
		{"creator edits all fields", 10, entity.RoleMember, full, nil},
		{"assignee changes status", 20, entity.RoleMember, status, nil},
		{"assignee cannot edit title", 20, entity.RoleMember, usecase.UpdateTask{Title: ptr("x")}, entity.ErrForbidden},
		{"assignee cannot edit description", 20, entity.RoleMember, usecase.UpdateTask{Description: ptr("x")}, entity.ErrForbidden},
		{"assignee cannot reassign", 20, entity.RoleMember, usecase.UpdateTask{SetAssignee: true, AssigneeID: ptr(int64(30))}, entity.ErrForbidden},
		{"assignee cannot unassign", 20, entity.RoleMember, usecase.UpdateTask{SetAssignee: true}, entity.ErrForbidden},
		{"assignee cannot mix status and title", 20, entity.RoleMember, usecase.UpdateTask{Status: ptr(entity.StatusDone), Title: ptr("x")}, entity.ErrForbidden},
		{"unrelated member cannot change status", 30, entity.RoleMember, status, entity.ErrForbidden},
		{"unrelated member cannot edit", 30, entity.RoleMember, full, entity.ErrForbidden},
		{"creator without membership cannot edit", 10, "", full, entity.ErrNotFound},
		{"assignee without membership cannot edit", 20, "", status, entity.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tasks := &taskAdapter{current: entity.Task{ID: 1, TeamID: 2, Title: "Before", CreatedBy: 10, AssigneeID: ptr(int64(20)), Status: entity.StatusTodo, Version: 1}}
			teams := &teamAdapter{roles: map[int64]entity.Role{30: entity.RoleMember}}
			if tc.role != "" {
				teams.roles[tc.actor] = tc.role
			}
			service := usecase.NewTask(tasks, teams, transactionAdapter{teams, tasks}, nil)
			tc.update.Version = 1
			updated, err := service.Update(t.Context(), tc.actor, 1, tc.update)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				if tasks.writes != 0 {
					t.Fatal("rejected update reached persistence")
				}
				return
			}
			if tasks.writes != 1 || updated.Version != 2 || tasks.actor != tc.actor {
				t.Fatalf("invalid persisted command: writes=%d task=%+v actor=%d", tasks.writes, updated, tasks.actor)
			}
			if tasks.previous.Title != "Before" || tasks.previous.Version != 1 {
				t.Fatalf("audit source is not the locked state: %+v", tasks.previous)
			}
		})
	}
}

func TestTaskUpdateLifecycle(t *testing.T) {
	tasks := &taskAdapter{current: entity.Task{ID: 1, TeamID: 2, Title: "Initial", Status: entity.StatusTodo, CreatedBy: 10, Version: 1, UpdatedAt: time.Unix(1, 0)}}
	teams := &teamAdapter{roles: map[int64]entity.Role{10: entity.RoleMember}}
	service := usecase.NewTask(tasks, teams, transactionAdapter{teams, tasks}, nil)
	done, err := service.Update(t.Context(), 10, 1, usecase.UpdateTask{Version: 1, Status: ptr(entity.StatusDone)})
	if err != nil {
		t.Fatal(err)
	}
	if done.ClosedAt == nil || !done.ClosedAt.Equal(done.UpdatedAt) {
		t.Fatal("closing and update timestamps must come from the same operation")
	}
	unchanged, err := service.Update(t.Context(), 10, 1, usecase.UpdateTask{Version: 2, Status: ptr(entity.StatusDone)})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Version != 2 || !unchanged.ClosedAt.Equal(*done.ClosedAt) || tasks.writes != 1 {
		t.Fatal("no-op must preserve version, timestamp and history")
	}
	_, err = service.Update(t.Context(), 10, 1, usecase.UpdateTask{Version: 1, Title: ptr("Stale")})
	if !errors.Is(err, entity.ErrConflict) || tasks.writes != 1 {
		t.Fatalf("stale version was written: %v", err)
	}
	reopened, err := service.Update(t.Context(), 10, 1, usecase.UpdateTask{Version: 2, Status: ptr(entity.StatusInProgress)})
	if err != nil || reopened.ClosedAt != nil || reopened.Version != 3 {
		t.Fatalf("invalid reopened task: %+v %v", reopened, err)
	}
	tasks.writeErr = errors.New("storage failed")
	_, err = service.Update(t.Context(), 10, 1, usecase.UpdateTask{Version: 3, Title: ptr("Must not succeed")})
	if !errors.Is(err, tasks.writeErr) || tasks.current.Title != "Initial" {
		t.Fatalf("storage failure hidden: %v", err)
	}
}

func TestTaskRejectsForeignAssignee(t *testing.T) {
	tasks := &taskAdapter{current: entity.Task{ID: 1, TeamID: 2, Title: "Initial", Status: entity.StatusTodo, CreatedBy: 10, Version: 1}}
	teams := &teamAdapter{roles: map[int64]entity.Role{10: entity.RoleMember}}
	service := usecase.NewTask(tasks, teams, transactionAdapter{teams, tasks}, nil)
	_, err := service.Update(t.Context(), 10, 1, usecase.UpdateTask{Version: 1, SetAssignee: true, AssigneeID: ptr(int64(99))})
	if !errors.Is(err, entity.ErrNotFound) || tasks.writes != 0 {
		t.Fatalf("foreign assignee accepted: %v", err)
	}
}

func TestListAuthorizesBeforeCache(t *testing.T) {
	cache := &cacheAdapter{hit: true}
	tasks := &taskAdapter{accessErr: entity.ErrNotFound}
	teams := &teamAdapter{}
	service := usecase.NewTask(tasks, teams, transactionAdapter{teams, tasks}, cache)
	_, err := service.List(t.Context(), 99, entity.TaskFilter{TeamID: 1, Pagination: entity.Pagination{Limit: 20}})
	if !errors.Is(err, entity.ErrNotFound) || cache.called || tasks.reads != 0 {
		t.Fatalf("unauthorized list reached data or cache: %v", err)
	}
}

func TestListUsesAuthorizedRevision(t *testing.T) {
	for _, hit := range []bool{true, false} {
		cache := &cacheAdapter{hit: hit, page: entity.TaskPage{Items: []entity.Task{{ID: 9}}, Pagination: entity.Pagination{Limit: 20}}}
		tasks := &taskAdapter{current: entity.Task{ID: 1}}
		teams := &teamAdapter{}
		service := usecase.NewTask(tasks, teams, transactionAdapter{teams, tasks}, cache)
		page, err := service.List(t.Context(), 10, entity.TaskFilter{TeamID: 2, Pagination: entity.Pagination{Limit: 20}})
		if err != nil || cache.revision != 7 {
			t.Fatalf("wrong list revision: %d %v", cache.revision, err)
		}
		if hit && (tasks.reads != 0 || page.Items[0].ID != 9) {
			t.Fatal("cache hit was ignored")
		}
		if !hit && (tasks.reads != 1 || cache.page.Items[0].ID != 1) {
			t.Fatal("cache miss was not filled")
		}
	}
}
