package entity_test

import (
	"testing"

	"github.com/bahr/secunda/internal/entity"
)

func TestTaskChangesCaptureOldAndNewValues(t *testing.T) {
	assignee := int64(20)
	old := entity.Task{Title: "Before", Status: entity.StatusTodo, AssigneeID: &assignee, Version: 1}
	updated := old
	updated.Title = "After"
	updated.AssigneeID = nil
	updated.Version = 2
	changes := updated.ChangesFrom(&old)
	if changes["title"].Old != "Before" || changes["title"].New != "After" {
		t.Fatalf("incorrect title history: %+v", changes)
	}
	if _, ok := changes["status"]; ok {
		t.Fatal("unchanged status included")
	}
	if _, ok := changes["assignee_id"]; !ok {
		t.Fatal("unassignment missing")
	}
}
