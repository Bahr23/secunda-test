package persistent

import "github.com/bahr/secunda/internal/entity"

// column and ascending are selected by repository code, never request text.
func pageSQL(p entity.Pagination, column string, ascending bool) (string, []any) {
	query, args := "", []any{}
	order, comparison := "DESC", "<"
	if ascending {
		order, comparison = "ASC", ">"
	}
	if p.Cursor && p.AfterID > 0 {
		query += " AND " + column + " " + comparison + " ?"
		args = append(args, p.AfterID)
	}
	query += " ORDER BY " + column + " " + order + " LIMIT ?"
	args = append(args, p.FetchLimit())
	if !p.Cursor {
		query += " OFFSET ?"
		args = append(args, p.Offset)
	}
	return query, args
}
