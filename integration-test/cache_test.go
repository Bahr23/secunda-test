//go:build integration

package integration_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/repo/cache"
	"github.com/bahr/secunda/internal/usecase"
)

func TestTaskCacheInvalidationAndFilters(t *testing.T) {
	f := newFixture(t)
	member := f.member(t, "Assignee", entity.RoleMember)
	task := f.task(t, &member.ID)
	filter := entity.TaskFilter{TeamID: f.team.ID, Pagination: entity.Pagination{Limit: 20}}
	oldPage, err := f.tasks.List(t.Context(), f.owner.ID, filter)
	if err != nil || len(oldPage.Items) != 1 {
		t.Fatalf("page: %+v %v", oldPage, err)
	}
	m, err := f.store.Tasks().ListAccess(t.Context(), f.team.ID, f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	adapter := testCache
	if _, ok := adapter.Get(t.Context(), filter, m.Revision); !ok {
		t.Fatal("task list was not cached")
	}
	keys, _, err := testRedis.Scan(t.Context(), 0, fmt.Sprintf("secunda:tasks:v1:team:%d:*", f.team.ID), 100).Result()
	if err != nil || len(keys) == 0 {
		t.Fatalf("cache keys: %v %v", keys, err)
	}
	for _, key := range keys {
		ttl, err := testRedis.TTL(t.Context(), key).Result()
		if err != nil || ttl < 290*time.Second || ttl > 300*time.Second {
			t.Fatalf("incorrect cache TTL: %v %v", ttl, err)
		}
	}
	_, err = f.tasks.Update(t.Context(), f.owner.ID, task.ID, usecase.UpdateTask{Version: 1, Status: pointer(entity.StatusDone)})
	if err != nil {
		t.Fatal(err)
	}
	// Emulate a slow reader repopulating an old key after the writer committed.
	adapter.Set(t.Context(), filter, m.Revision, oldPage)
	page, err := f.tasks.List(t.Context(), f.owner.ID, filter)
	if err != nil || len(page.Items) != 1 || page.Items[0].Status != entity.StatusDone {
		t.Fatalf("stale cache escaped revision: %+v %v", page, err)
	}
	filter.Status = entity.StatusTodo
	page, err = f.tasks.List(t.Context(), f.owner.ID, filter)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("status filter collision: %+v %v", page, err)
	}
	filter.Status = entity.StatusDone
	filter.AssigneeID = &member.ID
	page, err = f.tasks.List(t.Context(), f.owner.ID, filter)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("assignee filter: %+v %v", page, err)
	}
	filter.AssigneeID = &f.owner.ID
	page, err = f.tasks.List(t.Context(), f.owner.ID, filter)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("assignee cache collision: %+v %v", page, err)
	}
	filter.AssigneeID = nil
	filter.Offset = 1
	page, err = f.tasks.List(t.Context(), f.owner.ID, filter)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("pagination cache collision: %+v %v", page, err)
	}
	filter.Offset = 0
	closedCache, err := cache.NewTasks("redis://unused:6379", log, cache.Options{Timeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_ = closedCache.Close()
	fallback := usecase.NewTask(f.store.Tasks(), f.store.Teams(), f.store, closedCache)
	page, err = fallback.List(t.Context(), f.owner.ID, filter)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("Redis outage must fall back to MySQL: %+v %v", page, err)
	}
	_, err = fallback.Update(t.Context(), f.owner.ID, task.ID, usecase.UpdateTask{Version: 2, Status: pointer(entity.StatusTodo)})
	if err != nil {
		t.Fatal(err)
	}
	page, err = f.tasks.List(t.Context(), f.owner.ID, filter) // Redis has pre-outage keys.
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("stale cache resurrected after outage: %+v %v", page, err)
	}
	filter.Status = ""
	_ = f.task(t, nil)
	page, err = f.tasks.List(t.Context(), f.owner.ID, filter)
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("creation did not invalidate cache: %+v %v", page, err)
	}
}
