package v1_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bahr/secunda/internal/entity"
)

func TestCursorRoundTripAndScope(t *testing.T) {
	deps := dependencies(t)
	deps.Teams = teamAdapter{t: t, list: func(_ context.Context, _ int64, p entity.Pagination) (entity.TeamPage, error) {
		if !p.Cursor || p.Offset != 0 {
			t.Fatalf("not cursor pagination: %+v", p)
		}
		items := []entity.Team{{ID: 9}, {ID: 8}, {ID: 7}}
		if p.AfterID != 0 {
			if p.AfterID != 8 {
				t.Fatalf("wrong boundary: %d", p.AfterID)
			}
			items = items[2:]
		}
		return entity.NewPage(items, p, func(team entity.Team) int64 { return team.ID }), nil
	}}
	h := router(deps, io.Discard)
	first := request(h, "GET", "/api/v1/teams?cursor=&limit=2", "Bearer valid-token", "", "")
	var page struct {
		Items []entity.Team `json:"items"`
		Next  string        `json:"next_cursor"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if first.Code != 200 || len(page.Items) != 2 || page.Next == "" || strings.Contains(first.Body.String(), "next_id") {
		t.Fatalf("bad first page: %s", first.Body)
	}
	next := page.Next
	last := request(h, "GET", "/api/v1/teams?cursor="+next+"&limit=1", "Bearer valid-token", "", "")
	page.Next = ""
	if err := json.Unmarshal(last.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if last.Code != 200 || len(page.Items) != 1 || page.Items[0].ID != 7 || page.Next != "" {
		t.Fatalf("bad last page: %s", last.Body)
	}
	for _, query := range []string{
		"cursor=" + next + "&offset=0", "cursor=" + next + "&changed_filter=yes", "cursor=invalid",
		"cursor=" + strings.Repeat("A", len(next)), "cursor=&cursor=", "cursor=&limit=2&limit=3",
	} {
		assertError(t, request(h, "GET", "/api/v1/teams?"+query, "Bearer valid-token", "", ""), http.StatusBadRequest, "invalid_input")
	}
	assertError(t, request(h, "GET", "/api/v1/tasks?team_id=1&cursor="+next, "Bearer valid-token", "", ""), 400, "invalid_input")
}
