package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRemoteSOPNotesNeverAdmitAgentWork(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "busy"}[busy], func(t *testing.T) {
			f := newSupplementFixture(t, "claude", "running", true)
			dbfx.Exec(t, `UPDATE issue SET assignee_type='agent',assignee_id=$2 WHERE id=$1`, f.issueID, f.agentID)
			if !busy {
				dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1`, f.taskID)
			}
			var comment CommentResponse
			req := withURLParam(newRequest(http.MethodPost, "/comments", map[string]any{"content": "/note !sop inspect [\"--help\"]"}), "id", f.issueID)
			testutil.Call(t, testHandler.CreateComment, req).Want(http.StatusCreated).JSON(&comment)
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, f.issueID); n != 1 {
				t.Fatal("note admitted agent work")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM task_supplement WHERE task_id=$1`, f.taskID); n != 0 {
				t.Fatal("note steered an agent")
			}
			var requests []protocol.RemoteSOPRequest
			take := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/remote-sops/take", nil), "runtimeId", f.runtimeID)
			testutil.Call(t, testHandler.TakeRemoteSOPs, take).Want(http.StatusOK).JSON(&requests)
			if len(requests) != 1 || requests[0].ID != comment.ID {
				t.Fatalf("requests=%+v", requests)
			}
			dbfx.Cleanup(t, `DELETE FROM task_message WHERE task_id=$1`, comment.ID)
			dbfx.Cleanup(t, `DELETE FROM task_token WHERE task_id=$1`, comment.ID)
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE id=$1`, comment.ID)
			if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE id=$1`, comment.ID); n != 0 {
				t.Fatal("request not consumed")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND status IN ('queued','dispatched','running') AND id<>$2`, f.issueID, f.taskID); n != 0 {
				t.Fatal("remote call activated agent work")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='completed' AND started_at IS NULL AND dispatched_at IS NULL AND context->>'type'='remote_sop'`, comment.ID); n != 1 {
				t.Fatal("missing execution-only log")
			}
			testutil.Call(t, testHandler.TakeRemoteSOPs, take).Want(http.StatusOK).JSON(&requests)
			if len(requests) != 0 {
				t.Fatal("request consumed twice")
			}
		})
	}
}
