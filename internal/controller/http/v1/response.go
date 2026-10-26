package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/bahr/secunda/internal/entity"
	"github.com/bahr/secunda/pkg/logger"
)

type errorBody struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var body errorBody
	body.Error.Code = code
	body.Error.Message = message
	body.Error.RequestID = w.Header().Get("X-Request-ID")
	writeJSON(w, status, body)
}

func (h *handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, entity.ErrInvalid):
		writeError(w, 400, "invalid_input", err.Error())
	case errors.Is(err, entity.ErrUnauthenticated):
		writeError(w, 401, "unauthenticated", "invalid or missing credentials")
	case errors.Is(err, entity.ErrForbidden):
		writeError(w, 403, "forbidden", "operation is not permitted")
	case errors.Is(err, entity.ErrNotFound):
		writeError(w, 404, "not_found", "resource not found")
	case errors.Is(err, entity.ErrConflict):
		writeError(w, 409, "conflict", "resource already exists or task version is stale; reload and retry")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, 504, "timeout", "request timed out")
	default:
		logger.FromContext(r.Context(), h.log).ErrorContext(r.Context(), "request failed", slog.Any("error", err))
		writeError(w, 500, "internal_error", "internal server error")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, 415, "unsupported_media_type", "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	var raw json.RawMessage
	err = decoder.Decode(&raw)
	if err == nil {
		var extra any
		if trailing := decoder.Decode(&extra); trailing != io.EOF {
			err = trailing
			if err == nil {
				err = fmt.Errorf("body must contain exactly one JSON object")
			}
		}
	}
	if err == nil {
		object := bytes.TrimSpace(raw)
		if len(object) == 0 || object[0] != '{' {
			err = fmt.Errorf("body must be a JSON object")
		} else {
			decoder = json.NewDecoder(bytes.NewReader(object))
			decoder.DisallowUnknownFields()
			err = decoder.Decode(target)
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, 413, "body_too_large", "request body exceeds 1 MiB")
		} else {
			writeError(w, 400, "invalid_input", "invalid JSON body or unknown field")
		}
		return false
	}
	return true
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: invalid %s", entity.ErrInvalid, name)
	}
	return id, nil
}

func pagination(r *http.Request) (entity.Pagination, error) {
	p := entity.Pagination{Limit: 20}
	for key, target := range map[string]*int{"limit": &p.Limit, "offset": &p.Offset} {
		if r.URL.Query().Has(key) {
			n, err := strconv.Atoi(r.URL.Query().Get(key))
			if err != nil {
				return p, entity.ErrInvalid
			}
			*target = n
		}
	}
	if !p.Valid() {
		return p, fmt.Errorf("%w: limit must be 1–100 and offset non-negative", entity.ErrInvalid)
	}
	return p, nil
}
