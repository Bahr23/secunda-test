//go:build integration

package integration_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

type cursorPage struct {
	Items []struct {
		ID int64 `json:"id"`
	} `json:"items"`
	Next string `json:"next_cursor"`
}

func TestCursorPaginationUsesSQLBoundariesAndCachesPositions(t *testing.T) {
	f := newFixture(t)
	c := newAPI(t, f)
	token := tokenFor(t, f.owner.ID)
	var tasks []entity.Task
	for range 5 {
		tasks = append(tasks, f.task(t, nil))
	}
	path := fmt.Sprintf("/api/v1/tasks?team_id=%d&limit=2&cursor=", f.team.ID)
	first := decode[cursorPage](t, c.request("GET", path, token, nil, 200))
	if len(first.Items) != 2 || first.Items[0].ID != tasks[4].ID || first.Items[1].ID != tasks[3].ID || first.Next == "" {
		t.Fatalf("first page: %+v", first)
	}
	// An insertion before the current position must not shift the next page.
	_ = f.task(t, nil)
	second := decode[cursorPage](t, c.request("GET", path+first.Next, token, nil, 200))
	cached := decode[cursorPage](t, c.request("GET", path+first.Next, token, nil, 200))
	if !reflect.DeepEqual(second, cached) || len(second.Items) != 2 || second.Items[0].ID != tasks[2].ID || second.Items[1].ID != tasks[1].ID {
		t.Fatalf("second page: %+v cached=%+v", second, cached)
	}
	last := decode[cursorPage](t, c.request("GET", path+second.Next, token, nil, 200))
	if len(last.Items) != 1 || last.Items[0].ID != tasks[0].ID || last.Next != "" {
		t.Fatalf("last page: %+v", last)
	}
	c.request("GET", path+first.Next+"&status=done", token, nil, 400)
	foreign := newUser(t, f.store, "Foreign cursor reader")
	c.request("GET", path+first.Next, tokenFor(t, foreign.ID), nil, 400)
	c.request("GET", path, tokenFor(t, foreign.ID), nil, 404)
	filter := entity.TaskFilter{TeamID: f.team.ID, Pagination: entity.Pagination{Limit: 2, Cursor: true}}
	page, err := f.tasks.List(t.Context(), f.owner.ID, filter)
	if err != nil || page.NextID == 0 {
		t.Fatalf("cursor read: %+v %v", page, err)
	}
	cacheHit, err := f.tasks.List(t.Context(), f.owner.ID, filter)
	if err != nil || !reflect.DeepEqual(page, cacheHit) {
		t.Fatalf("cached cursor lost metadata: %+v %v", cacheHit, err)
	}
}

func TestAllCollectionsSupportCursorPagination(t *testing.T) {
	f := newFixture(t)
	c := newAPI(t, f)
	token := tokenFor(t, f.owner.ID)
	task := f.task(t, nil)
	for i := range 4 {
		var err error
		task, err = f.tasks.Update(t.Context(), f.owner.ID, task.ID, usecase.UpdateTask{Version: task.Version, Title: pointer(fmt.Sprintf("Title %d", i))})
		if err != nil {
			t.Fatal(err)
		}
	}
	for range 5 {
		if _, err := f.tasks.Comment(t.Context(), f.owner.ID, task.ID, "Comment"); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 4 {
		if _, err := f.teams.Create(t.Context(), f.owner.ID, fmt.Sprintf("Team %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		path      string
		ascending bool
	}{
		{"/api/v1/teams", false},
		{fmt.Sprintf("/api/v1/tasks/%d/history", task.ID), true},
		{fmt.Sprintf("/api/v1/tasks/%d/comments", task.ID), true},
	} {
		t.Run(tc.path, func(t *testing.T) {
			next, seen := "", []int64{}
			for pageNumber := 0; ; pageNumber++ {
				if pageNumber > 5 {
					t.Fatal("cursor did not terminate")
				}
				page := decode[cursorPage](t, c.request("GET", tc.path+"?limit=2&cursor="+next, token, nil, 200))
				if len(page.Items) > 2 {
					t.Fatal("lookahead row leaked")
				}
				for _, row := range page.Items {
					seen = append(seen, row.ID)
				}
				if page.Next == "" {
					break
				}
				next = page.Next
			}
			if len(seen) != 5 {
				t.Fatalf("got %d rows, want 5", len(seen))
			}
			for i := 1; i < len(seen); i++ {
				if seen[i] == seen[i-1] || (seen[i] > seen[i-1]) != tc.ascending {
					t.Fatal("duplicate or incorrect order")
				}
			}
		})
	}
}
