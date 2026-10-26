package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/internal/usecase"
)

// optionalID distinguishes an omitted assignee from explicit null (unassign).
type optionalID struct {
	Set   bool
	Value *int64
}

func (id *optionalID) UnmarshalJSON(data []byte) error {
	id.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		id.Value = nil
		return nil
	}
	var value int64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	id.Value = &value
	return nil
}

func (h *handler) createTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TeamID      int64  `json:"team_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		AssigneeID  *int64 `json:"assignee_id"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	task, err := h.tasks.Create(r.Context(), actor(r), usecase.CreateTask{TeamID: in.TeamID, Title: in.Title, Description: in.Description, AssigneeID: in.AssigneeID})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, 201, task)
}

func (h *handler) listTasks(w http.ResponseWriter, r *http.Request) {
	p, err := h.pagination(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	teamID, err := strconv.ParseInt(r.URL.Query().Get("team_id"), 10, 64)
	if err != nil || teamID <= 0 {
		h.fail(w, r, entity.ErrInvalid)
		return
	}
	f := entity.TaskFilter{TeamID: teamID, Status: entity.Status(r.URL.Query().Get("status")), Pagination: p}
	if r.URL.Query().Has("assignee_id") {
		id, err := strconv.ParseInt(r.URL.Query().Get("assignee_id"), 10, 64)
		if err != nil || id <= 0 {
			h.fail(w, r, entity.ErrInvalid)
			return
		}
		f.AssigneeID = &id
	}
	page, err := h.tasks.List(r.Context(), actor(r), f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePage(w, r, h, page)
}

func (h *handler) updateTask(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in struct {
		Version     int64          `json:"version"`
		Title       *string        `json:"title"`
		Description *string        `json:"description"`
		Status      *entity.Status `json:"status"`
		AssigneeID  optionalID     `json:"assignee_id"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	task, err := h.tasks.Update(r.Context(), actor(r), id, usecase.UpdateTask{Version: in.Version, Title: in.Title,
		Description: in.Description, Status: in.Status, AssigneeID: in.AssigneeID.Value, SetAssignee: in.AssigneeID.Set})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, 200, task)
}

func (h *handler) history(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, err := h.pagination(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items, err := h.tasks.History(r.Context(), actor(r), id, p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePage(w, r, h, items)
}

func (h *handler) addComment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	comment, err := h.tasks.Comment(r.Context(), actor(r), id, in.Content)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, 201, comment)
}

func (h *handler) comments(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, err := h.pagination(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items, err := h.tasks.Comments(r.Context(), actor(r), id, p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePage(w, r, h, items)
}
