package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func createStagedChild(t *testing.T, parentID string, stage int, status string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "staged child " + time.Now().Format(time.RFC3339Nano), "status": status,
		"parent_issue_id": parentID, "stage": stage,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create staged child: %d %s", w.Code, w.Body.String())
	}
	var child IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&child); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupChildDoneIssue(child.ID) })
	return child
}

func TestAutomaticChildWakeupsRetired(t *testing.T) {
	parent := dbfx.Issue(t, "Plugin-owned parent", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": handlerTestAgentID(t)})
	child := dbfx.Issue(t, "Plugin-owned child", testutil.Cols{"parent_issue_id": parent, "status": "in_progress"})
	dbfx.Cleanup(t, "DELETE FROM issue_child_event WHERE parent_id=$1", parent)
	updateChildStatus(t, child, "done")
	runWakeupTick(t)
	if n := dbfx.Count(t, "SELECT count(*) FROM issue_wakeup WHERE issue_id=$1 AND system_rule IS NOT NULL", parent); n != 0 {
		t.Fatalf("created %d built-in rules", n)
	}
	if n := countPendingTasksForAgent(t, parent, handlerTestAgentID(t)); n != 0 {
		t.Fatalf("built-in wakeup queued %d runs", n)
	}
	var rows []any
	testutil.Call(t, testHandler.ListIssueSystemWakeups, withURLParam(newRequest("GET", "/api/issues/"+parent+"/system-wakeups", nil), "id", parent)).Want(http.StatusOK).JSON(&rows)
	if len(rows) != 0 {
		t.Fatal("retired issue settings still exposed")
	}
	testutil.Call(t, testHandler.ListWorkspaceSystemWakeups, newRequest("GET", "/api/system-wakeups", nil)).Want(http.StatusOK).JSON(&rows)
	if len(rows) != 0 {
		t.Fatal("retired workspace settings still exposed")
	}
	testutil.Call(t, testHandler.UpdateIssueSystemWakeup, withURLParam(newRequest("PUT", "/api/issues/"+parent+"/system-wakeups/child_done", map[string]any{"enabled": true}), "id", parent)).Want(http.StatusGone)
	testutil.Call(t, testHandler.UpdateWorkspaceSystemWakeup, newRequest("PUT", "/api/system-wakeups/child_done", map[string]any{"enabled": true})).Want(http.StatusGone)
}
