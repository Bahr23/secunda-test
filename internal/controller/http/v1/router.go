package v1

import (
	"crypto/rand"
	"log/slog"
	"net/http"
	"time"

	"github.com/bahr/secunda/docs"
)

type handler struct {
	auth    Authentication
	teams   TeamOperations
	tasks   TaskOperations
	tokens  TokenVerifier
	log     *slog.Logger
	timeout time.Duration
	cursors cursorCodec
	limits  *requestLimits
}

func New(deps Dependencies, options Options) http.Handler {
	h := &handler{auth: deps.Auth, teams: deps.Teams, tasks: deps.Tasks, tokens: deps.Tokens, log: options.Logger, timeout: options.RequestTimeout}
	secret := options.CursorSecret
	if len(secret) == 0 {
		secret = []byte(rand.Text())
	}
	h.cursors = newCursorCodec(secret)
	if options.RateLimit != nil {
		h.limits = newRequestLimits(*options.RateLimit)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/register", h.limitAuth(http.HandlerFunc(h.register)))
	mux.Handle("POST /api/v1/login", h.limitAuth(http.HandlerFunc(h.login)))
	protected := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, h.authenticate(h.limitUser(fn))) }
	protected("POST /api/v1/teams", h.createTeam)
	protected("GET /api/v1/teams", h.listTeams)
	protected("POST /api/v1/teams/{id}/invite", h.invite)
	protected("PATCH /api/v1/teams/{id}/members/{user_id}", h.changeRole)
	protected("GET /api/v1/teams/{id}/stats", h.stats)
	protected("POST /api/v1/tasks", h.createTask)
	protected("GET /api/v1/tasks", h.listTasks)
	protected("PUT /api/v1/tasks/{id}", h.updateTask)
	protected("GET /api/v1/tasks/{id}/history", h.history)
	protected("POST /api/v1/tasks/{id}/comments", h.addComment)
	protected("GET /api/v1/tasks/{id}/comments", h.comments)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := deps.Ready(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "database is unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(docs.OpenAPI)
	})
	mux.HandleFunc("GET /swagger", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(docs.SwaggerUI)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
	})
	return h.observe(h.limitIP(mux))
}
