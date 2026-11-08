package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRemoteSOPConsumePreservesTask(t *testing.T) {
	f := newSupplementFixture(t, "codex", "running", true)
	dbfx.Exec(t, `UPDATE issue SET assignee_type='agent',assignee_id=$2 WHERE id=$1`, f.issueID, f.agentID)
	var comment CommentResponse
	supplementRequest(t, f, "0199a4e8-22ce-7b01-bba5-123456789123", "!sop inspect [\"--help\"]").Want(http.StatusCreated).JSON(&comment)
	_, err := testHandler.Queries.ClaimNextTaskSupplement(t.Context(), db.ClaimNextTaskSupplementParams{TaskID: parseUUID(f.taskID), IncludeAgentAuthored: true})
	if err != nil {
		t.Fatal(err)
	}
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/remote-sop/consume?comment_id="+comment.ID, nil), "taskId", f.taskID)
	testutil.Call(t, testHandler.ConsumeRemoteSOP, req).Want(http.StatusNoContent)
	if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE id=$1`, comment.ID); n != 0 {
		t.Fatal("request remains in comments")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='running'`, f.taskID); n != 1 {
		t.Fatal("consuming request cancelled task")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement WHERE comment_id=$1`, comment.ID); n != 0 {
		t.Fatal("receipt remains after consumption")
	}
}
