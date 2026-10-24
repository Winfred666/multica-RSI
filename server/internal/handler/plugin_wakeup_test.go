package handler

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
)

func TestPluginWakeupIdempotency(t *testing.T) {
	issue := dbfx.Issue(t, "Idempotent plugin wakeup")
	agent := dbfx.Agent(t, "Plugin target", testRuntimeID)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup_receipt WHERE wakeup_id IN (SELECT id FROM issue_wakeup WHERE issue_id=$1)", issue)
	dbfx.Cleanup(t, "DELETE FROM issue_wakeup WHERE issue_id=$1", issue)
	dbfx.Cleanup(t, "DELETE FROM activity_log WHERE issue_id=$1", issue)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	input := service.WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 1, Instruction: "Review child result", IdempotencyKey: "child:review:one"}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errors := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			row, err := svc.Create(context.Background(), parseUUID(issue), parseUUID(testUserID), pgtype.UUID{}, input)
			if err != nil {
				errors <- err
				return
			}
			ids <- uuidToString(row.ID)
		})
	}
	wg.Wait()
	close(ids)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	var id string
	for next := range ids {
		if id != "" && id != next {
			t.Fatal("concurrent deliveries created duplicate wakeups")
		}
		id = next
	}
	// A retry after the one-shot was consumed must not rearm it.
	dbfx.Exec(t, "UPDATE issue_wakeup SET enabled=false WHERE id=$1", id)
	row, err := svc.Create(context.Background(), parseUUID(issue), parseUUID(testUserID), pgtype.UUID{}, input)
	if err != nil || row.Enabled || uuidToString(row.ID) != id {
		t.Fatal("retry rearmed the consumed wakeup", err)
	}
	input.IdempotencyKey = "child:review:two"
	row, err = svc.Create(context.Background(), parseUUID(issue), parseUUID(testUserID), pgtype.UUID{}, input)
	if err != nil || uuidToString(row.ID) == id {
		t.Fatal("a new transition did not get its own wakeup", err)
	}
}
