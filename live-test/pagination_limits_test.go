//go:build live

package live_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/bahr/secunda/internal/entity"
)

func (s *suite) cursors(t *testing.T, f fixture) {
	team := s.team(t, f.owner, "cursors")
	first := s.task(t, f.owner, team.ID, "First", nil)
	second := s.task(t, f.owner, team.ID, "Second", nil)
	third := s.task(t, f.owner, team.ID, "Third", nil)
	path := listPath(team.ID) + "&limit=2&cursor="
	p := decode[page[entity.Task]](t, s.request(t, "GET", path, f.owner, nil, 200))
	if len(p.Items) != 2 || p.Items[0].ID != third.ID || p.Items[1].ID != second.ID || p.NextCursor == "" {
		t.Fatal("incorrect first cursor page")
	}
	s.task(t, f.owner, team.ID, "Inserted while paginating", nil)
	for range 2 {
		next := decode[page[entity.Task]](t, s.request(t, "GET", path+p.NextCursor, f.owner, nil, 200))
		if len(next.Items) != 1 || next.Items[0].ID != first.ID || next.NextCursor != "" {
			t.Fatal("cursor repeated or skipped an existing task after insertion")
		}
	}
	s.request(t, "GET", path+p.NextCursor+"&status=done", f.owner, nil, 400)
	s.request(t, "GET", path+p.NextCursor+"&offset=0", f.owner, nil, 400)
	s.request(t, "GET", path+"invalid", f.owner, nil, 400)
	s.request(t, "GET", path+p.NextCursor, f.outsider, nil, 400)
	s.request(t, "GET", path, f.outsider, nil, 404)
}

func (s *suite) rateLimits(t *testing.T) {
	for _, tc := range []struct {
		method, path, contentType string
		normal, attempts          int
	}{
		{"POST", "/api/v1/login", "text/plain", 415, 30},
		{"GET", "/api/v1/nonexistent", "", 404, 200},
	} {
		limited := false
		for range tc.attempts {
			r := s.rawOnce(t, tc.method, tc.path, "", tc.contentType, nil)
			if r.status == http.StatusTooManyRequests {
				expect(t, r, 429)
				if r.header.Get("Retry-After") == "" {
					t.Fatal("rate limit missing retry delay")
				}
				limited = true
				break
			}
			expect(t, r, tc.normal)
		}
		if !limited {
			t.Fatalf("%s was not rate limited with default settings", tc.path)
		}
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		expect(t, s.rawOnce(t, "GET", path, "", "", nil), 200)
	}
	// Allow the local peer's general budget to begin refilling for interactive use.
	time.Sleep(time.Second)
}
