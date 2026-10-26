//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/bahr/secunda/internal/entity"
)

func TestHistoryPreservesJSONValues(t *testing.T) {
	f := newFixture(t)
	task := f.task(t, nil)
	closedAt := time.Date(2026, time.October, 26, 9, 30, 0, 123456000, time.UTC)
	changes := map[string]entity.Change{
		"version":     {Old: int64(9007199254740993), New: int64(9223372036854775807)},
		"assignee_id": {Old: nil, New: int64(9007199254740995)},
		"description": {Old: "Before\n\"quoted\"", New: "Изменение задачи"},
		"closed_at":   {Old: nil, New: closedAt},
	}
	want, err := json.Marshal(changes)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture intentionally includes values beyond float64's exact integer range.
	result, err := testDB.ExecContext(t.Context(),
		`INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES (?, ?, ?, ?)`,
		task.ID, f.owner.ID, want, closedAt)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	history, err := f.store.Tasks().History(t.Context(), task.ID, entity.Pagination{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[1].ID != id {
		t.Fatalf("expected creation and inserted history, got %+v", history)
	}
	got, err := json.Marshal(history[1].Changes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("history changed JSON values during round-trip:\nwant %s\n got %s", want, got)
	}
}
