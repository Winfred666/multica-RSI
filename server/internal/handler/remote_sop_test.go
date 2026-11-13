package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRemoteSOPNotesNeverAdmitAgentWork(t *testing.T) {
	for _, scenario := range []struct {
		name, callerType string
		busy             bool
	}{{"member-idle", "member", false}, {"member-busy", "member", true}, {"agent", "agent", false}} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newSupplementFixture(t, "claude", "running", true)
			cwd := t.TempDir()
			dbfx.Exec(t, `UPDATE agent_task_queue SET work_dir=$2,session_id=NULL WHERE id=$1`, f.taskID, cwd)
			userID := dbfx.User(t, "SOP caller", "sop-"+f.issueID+"@example.com")
			dbfx.Member(t, testWorkspaceID, userID, "member")
			dbfx.Exec(t, `UPDATE issue SET assignee_type='agent',assignee_id=$2 WHERE id=$1`, f.issueID, f.agentID)
			if !scenario.busy {
				dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1`, f.taskID)
			}
			var comment CommentResponse
			req := withURLParam(newRequest(http.MethodPost, "/comments", map[string]any{"content": "/note !sop-remote inspect {\"args\":[\"--help\"]}"}), "id", f.issueID)
			req.Header.Set("X-User-ID", userID)
			callerID := userID
			if scenario.callerType == "agent" {
				callerID = createHandlerTestAgent(t, "SOP caller", nil)
				sourceID := createHandlerTestTaskForAgent(t, callerID)
				dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=$2,accountable_user_id=$2 WHERE id=$1`, sourceID, testUserID)
				req.Header.Set("X-Agent-ID", callerID)
				req.Header.Set("X-Task-ID", sourceID)
			}
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
			if requests[0].WorkDir != cwd || requests[0].AgentID != f.agentID || requests[0].CallerType != scenario.callerType || requests[0].CallerID != callerID {
				t.Fatal("missing comment caller")
			}
			dbfx.Cleanup(t, `DELETE FROM task_message WHERE task_id=$1`, comment.ID)
			dbfx.Cleanup(t, `DELETE FROM task_token WHERE token_hash=$1`, auth.HashToken(requests[0].AuthToken))
			var tokenUserID string
			dbfx.QueryRow(t, `SELECT user_id::text FROM task_token WHERE token_hash=$1`, auth.HashToken(requests[0].AuthToken)).Scan(&tokenUserID)
			if tokenUserID != userID {
				t.Fatal("credentials borrowed another user's authority")
			}
			authed := middleware.Auth(testHandler.Queries, nil, nil, nil)(http.HandlerFunc(testHandler.CreateComment))
			callback := withURLParam(newRequest(http.MethodPost, "/comments", map[string]any{"content": "/note !sop-remote inspect {\"args\":[\"--help\"]}"}), "id", f.issueID)
			callback.Header.Set("Authorization", "Bearer "+requests[0].AuthToken)
			var posted CommentResponse
			testutil.Call(t, authed.ServeHTTP, callback).Want(http.StatusCreated).JSON(&posted)
			if posted.AuthorType != scenario.callerType || posted.AuthorID != callerID {
				t.Fatalf("callback actor=%s/%s", posted.AuthorType, posted.AuthorID)
			}
			callback.Header.Set("X-Workspace-ID", "00000000-0000-4000-8000-000000000001")
			testutil.Call(t, authed.ServeHTTP, callback).Want(http.StatusForbidden)
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE id=$1`, comment.ID)
			if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE id=$1`, comment.ID); n != 1 {
				t.Fatal("request was deleted before questionnaire cleanup")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND status IN ('queued','dispatched','running') AND id<>$2`, f.issueID, f.taskID); n != 0 {
				t.Fatal("remote call activated agent work")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='completed' AND started_at IS NULL AND dispatched_at IS NULL AND context->>'type'='remote_sop'`, comment.ID); n != 1 {
				t.Fatal("missing execution-only log")
			}
			testutil.Call(t, testHandler.TakeRemoteSOPs, take).Want(http.StatusOK).JSON(&requests)
			if len(requests) != 1 || requests[0].ID != posted.ID || requests[0].CallerID != callerID {
				t.Fatal("callback lost caller provenance")
			}
			dbfx.Cleanup(t, `DELETE FROM task_token WHERE token_hash=$1`, auth.HashToken(requests[0].AuthToken))
			dbfx.Cleanup(t, `DELETE FROM task_message WHERE task_id=$1`, posted.ID)
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE id=$1`, posted.ID)
			testutil.Call(t, testHandler.TakeRemoteSOPs, take).Want(http.StatusOK).JSON(&requests)
			if len(requests) != 0 {
				t.Fatal("request consumed twice")
			}
		})
	}
}
