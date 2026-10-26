package entity

// Cursor pagination uses an exclusive ID boundary. Encoding and authenticating
// the public cursor belongs to the transport; SQL only needs the position.
type Pagination struct {
	Limit   int   `json:"limit"`
	Offset  int   `json:"offset"`
	Cursor  bool  `json:"-"`
	AfterID int64 `json:"-"`
}

func (p Pagination) Valid() bool {
	return p.Limit >= 1 && p.Limit <= 100 && p.Offset >= 0 && p.AfterID >= 0 &&
		((p.Cursor && p.Offset == 0) || (!p.Cursor && p.AfterID == 0))
}

// FetchLimit includes one lookahead row to determine whether another page exists.
func (p Pagination) FetchLimit() int {
	if p.Cursor {
		return p.Limit + 1
	}
	return p.Limit
}

type Page[T any] struct {
	Items []T `json:"items"`
	Pagination
	NextID int64 `json:"next_id,omitempty"` // Internal cache representation, never a public cursor.
}

type TaskPage = Page[Task]
type TeamPage = Page[Team]
type HistoryPage = Page[History]
type CommentPage = Page[Comment]

func NewPage[T any](items []T, p Pagination, id func(T) int64) Page[T] {
	if items == nil {
		items = []T{}
	}
	page := Page[T]{Items: items, Pagination: p}
	if p.Cursor && len(items) > p.Limit {
		page.Items = items[:p.Limit]
		page.NextID = id(page.Items[len(page.Items)-1])
	}
	return page
}
