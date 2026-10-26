package v1

import (
	"net/http"

	"github.com/bahr/secunda/internal/entity"
)

func (h *handler) createTeam(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	t, err := h.teams.Create(r.Context(), actor(r), in.Name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, 201, t)
}

func (h *handler) listTeams(w http.ResponseWriter, r *http.Request) {
	p, err := h.pagination(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	teams, err := h.teams.List(r.Context(), actor(r), p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePage(w, r, h, teams)
}

func (h *handler) invite(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in struct {
		UserID int64       `json:"user_id"`
		Role   entity.Role `json:"role"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Role == "" {
		in.Role = entity.RoleMember
	}
	if err := h.teams.Invite(r.Context(), actor(r), id, in.UserID, in.Role); err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, 201, entity.Membership{TeamID: id, UserID: in.UserID, Role: in.Role})
}

func (h *handler) changeRole(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	userID, err := pathID(r, "user_id")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in struct {
		Role entity.Role `json:"role"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := h.teams.ChangeRole(r.Context(), actor(r), id, userID, in.Role); err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, 200, entity.Membership{TeamID: id, UserID: userID, Role: in.Role})
}

func (h *handler) stats(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	stats, err := h.teams.Stats(r.Context(), actor(r), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, 200, stats)
}
