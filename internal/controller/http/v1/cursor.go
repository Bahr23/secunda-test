package v1

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/bahr/secunda/internal/entity"
)

// Scope binds a cursor to its actor, resource and filters, but allows changing
// the page size. It is never an authorization credential: usecases still check
// membership on every request, including a shared-cache hit.
func cursorScope(r *http.Request) string {
	query := r.URL.Query()
	query.Del("cursor")
	query.Del("limit")
	query.Del("offset")
	return strconv.FormatInt(actor(r), 10) + ":" + r.URL.EscapedPath() + "?" + query.Encode()
}

type cursorCodec struct{ key []byte }

func newCursorCodec(secret []byte) cursorCodec {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("secunda/pagination/v1"))
	return cursorCodec{key: mac.Sum(nil)}
}

func (c cursorCodec) sign(position []byte, scope string) []byte {
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(position)
	_, _ = mac.Write([]byte(scope))
	return mac.Sum(nil)
}

func (c cursorCodec) encode(id int64, scope string) string {
	position := make([]byte, 9)
	position[0] = 1
	binary.BigEndian.PutUint64(position[1:], uint64(id))
	return base64.RawURLEncoding.EncodeToString(append(position, c.sign(position, scope)...))
}

func (c cursorCodec) decode(raw, scope string) (int64, error) {
	if len(raw) != base64.RawURLEncoding.EncodedLen(41) {
		return 0, entity.ErrInvalid
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(data) != 41 || data[0] != 1 || !hmac.Equal(data[9:], c.sign(data[:9], scope)) {
		return 0, entity.ErrInvalid
	}
	id := binary.BigEndian.Uint64(data[1:9])
	if id == 0 || id > math.MaxInt64 {
		return 0, entity.ErrInvalid
	}
	return int64(id), nil
}

func (h *handler) pagination(r *http.Request) (entity.Pagination, error) {
	p, err := pagination(r)
	if err != nil {
		return p, err
	}
	query := r.URL.Query()
	for key, values := range query {
		if len(values) != 1 {
			return p, fmt.Errorf("%w: repeated query parameter %s", entity.ErrInvalid, key)
		}
	}
	if !query.Has("cursor") {
		return p, nil
	}
	if query.Has("offset") {
		return p, fmt.Errorf("%w: cursor and offset are mutually exclusive", entity.ErrInvalid)
	}
	p.Cursor = true
	if raw := query.Get("cursor"); raw != "" {
		p.AfterID, err = h.cursors.decode(raw, cursorScope(r))
		if err != nil {
			return p, fmt.Errorf("%w: invalid cursor for this resource and filters", entity.ErrInvalid)
		}
	}
	return p, nil
}

func writePage[T any](w http.ResponseWriter, r *http.Request, h *handler, page entity.Page[T]) {
	next := ""
	if page.Cursor && page.NextID > 0 {
		next = h.cursors.encode(page.NextID, cursorScope(r))
	}
	writeJSON(w, http.StatusOK, struct {
		Items      []T    `json:"items"`
		Limit      int    `json:"limit"`
		Offset     int    `json:"offset"`
		NextCursor string `json:"next_cursor,omitempty"`
	}{page.Items, page.Limit, page.Offset, next})
}
