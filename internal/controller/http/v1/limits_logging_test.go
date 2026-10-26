package v1_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	v1 "github.com/bahr/secunda/internal/controller/http/v1"
	"github.com/bahr/secunda/internal/entity"
)

func limitedRouter(t *testing.T, out io.Writer) http.Handler {
	t.Helper()
	deps := dependencies(t)
	deps.Teams = teamAdapter{t: t, list: func(_ context.Context, _ int64, p entity.Pagination) (entity.TeamPage, error) {
		return entity.TeamPage{Items: []entity.Team{}, Pagination: p}, nil
	}}
	return v1.New(deps, v1.Options{Logger: slog.New(slog.NewJSONHandler(out, nil)), RequestTimeout: time.Second,
		RateLimit: &v1.RateLimitOptions{RequestsPerSecond: 0.01, Burst: 2, AuthRequestsPerSecond: 0.01, AuthBurst: 1, MaxKeys: 100, IdleTTL: time.Hour}})
}

func TestRateLimitPrecedesAuthenticationAndIgnoresForwardedHeaders(t *testing.T) {
	h := limitedRouter(t, io.Discard)
	for i, status := range []int{401, 401, 429} {
		r := httptest.NewRequest("GET", "/api/v1/teams", nil)
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.0.%d", i))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("request %d: status=%d want=%d", i, w.Code, status)
		}
		if status == 429 {
			assertError(t, w, 429, "rate_limited")
			if w.Header().Get("Retry-After") == "" {
				t.Fatal("missing Retry-After")
			}
		}
	}
	for _, path := range []string{"/healthz", "/readyz", "/swagger", "/openapi.yaml"} {
		if w := request(h, "GET", path, "", "", ""); w.Code != 200 {
			t.Fatalf("throttling blocked %s", path)
		}
	}
}

func TestAuthRoutesShareBudgetBeforeBusinessLogic(t *testing.T) {
	h := limitedRouter(t, io.Discard)
	w := request(h, "POST", "/api/v1/login", "", "text/plain", "private-password")
	assertError(t, w, 415, "unsupported_media_type")
	w = request(h, "POST", "/api/v1/register", "", "text/plain", "private-password")
	assertError(t, w, 429, "rate_limited")
}

func TestUserLimitSurvivesPeerAddressChanges(t *testing.T) {
	h := limitedRouter(t, io.Discard)
	for i, status := range []int{200, 200, 429} {
		r := httptest.NewRequest("GET", "/api/v1/teams", nil)
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", i+1)
		r.Header.Set("Authorization", "Bearer valid-token")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("request %d: status=%d want=%d", i, w.Code, status)
		}
	}
}

func TestStructuredAccessLogsIncludeActorAndSafeFields(t *testing.T) {
	var output bytes.Buffer
	h := limitedRouter(t, &output)
	w := request(h, "GET", "/api/v1/teams?private-query=do-not-log", "Bearer valid-token", "", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var event map[string]any
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event["event"] != "http.request" || event["user_id"] != float64(42) || event["route"] != "GET /api/v1/teams" || event["status"] != float64(200) || event["duration_ms"] == nil || event["request_id"] != w.Header().Get("X-Request-ID") {
		t.Fatalf("incorrect log event: %v", event)
	}
	if strings.Contains(output.String(), "valid-token") || strings.Contains(output.String(), "do-not-log") {
		t.Fatal("access log leaked request secrets")
	}
	output.Reset()
	request(h, "GET", "/api/v1/teams", "", "", "")
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event["level"] != "WARN" {
		t.Fatal("client errors must be distinguishable in structured logs")
	}
}
