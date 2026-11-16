package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestRemoteSOPUsesOrdinaryTaskDelivery(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newSupplementFixture(t, "claude", "running", true)
	dbfx.Exec(t, `UPDATE agent_task_queue SET session_id=NULL,status='completed',completed_at=now() WHERE id=$1`, f.taskID)
	dbfx.Exec(t, `UPDATE issue SET assignee_type='agent',assignee_id=$2 WHERE id=$1`, f.issueID, f.agentID)
	question := dbfx.Comment(t, f.issueID, "/note !sop-render inspect {}\n{\"args\":[]}")
	peer := createHandlerTestAgent(t, "Not a command recipient", nil)
	for _, content := range []string{"/note !sop-remote inspect {}"} {
		testutil.Call(t, testHandler.CreateComment, withURLParam(newRequest(http.MethodPost, "/comments", map[string]any{"content": content}), "id", f.issueID)).Want(http.StatusCreated)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, f.issueID); n != 1 {
		t.Fatal("inert text admitted work")
	}
	var comments []CommentResponse
	for range 2 {
		var posted CommentResponse
		testutil.Call(t, testHandler.CreateComment, withURLParam(newRequest(http.MethodPost, "/comments", map[string]any{
			"content": fmt.Sprintf("!sop-remote inspect malformed [@Peer](mention://agent/%s)", peer), "parent_id": question,
		}), "id", f.issueID)).Want(http.StatusCreated).JSON(&posted)
		comments = append(comments, posted)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND status='queued' AND context->>'type'='remote_sop' AND cardinality(coalesced_comment_ids)=0`, f.issueID); n != 2 {
		t.Fatalf("independent commands queued=%d", n)
	}
	if n := countQueuedCommentTriggerTasks(t, f.issueID, peer); n != 0 {
		t.Fatal("command also admitted a mentioned model")
	}
	for i, comment := range comments {
		var response struct {
			Task *AgentTaskResponse `json:"task"`
		}
		req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/tasks/claim", nil), "runtimeId", f.runtimeID)
		testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&response)
		task := response.Task
		if task == nil || task.TriggerCommentID == nil || *task.TriggerCommentID != comment.ID || task.TriggerCommentContent != comment.Content || task.AuthToken == "" {
			t.Fatalf("command delivery=%+v", task)
		}
		testutil.Call(t, testHandler.StartTask, withURLParam(newOwnerDaemonRequest(http.MethodPost, "/tasks/"+task.ID+"/start", map[string]any{}), "taskId", task.ID)).Want(http.StatusOK)
		if i == 1 {
			authedDelete := middleware.Auth(testHandler.Queries, nil, nil, nil)(http.HandlerFunc(testHandler.DeleteComment))
			remove := withURLParam(newRequest(http.MethodDelete, "/comments/"+comment.ID+"/keep-replies", nil), "commentId", comment.ID)
			remove.Header.Set("Authorization", "Bearer "+task.AuthToken)
			testutil.Call(t, authedDelete.ServeHTTP, remove).Want(http.StatusNoContent)
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='cancelled'`, task.ID); n != 1 {
				t.Fatal("deleting a command did not cancel it normally")
			}
			continue
		}
		authedUpdate := middleware.Auth(testHandler.Queries, nil, nil, nil)(http.HandlerFunc(testHandler.UpdateComment))
		update := withURLParam(newRequest(http.MethodPut, "/comments/"+question, map[string]any{"content": "/note !sop-render inspect {}\n{\"args\":[\"accepted\"]}"}), "commentId", question)
		update.Header.Set("Authorization", "Bearer "+task.AuthToken)
		testutil.Call(t, authedUpdate.ServeHTTP, update).Want(http.StatusOK)
		testutil.Call(t, testHandler.CompleteTask, withURLParam(newOwnerDaemonRequest(http.MethodPost, "/tasks/"+task.ID+"/complete", map[string]any{"output": ""}), "taskId", task.ID)).Want(http.StatusOK)
		if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='completed' AND started_at IS NOT NULL`, task.ID); n != 1 {
			t.Fatal("command did not use ordinary completion")
		}
		if n := dbfx.Count(t, `SELECT count(*) FROM comment WHERE id=$1 AND deleted_at IS NULL`, comment.ID); n != 1 {
			t.Fatal("completion removed the command comment")
		}
	}
}

func TestRemoteSOPPreservesInvokePermission(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	privateID, _, _ := privateAgentTestFixture(t)
	issue := createCommentTriggerPreviewIssue(t, "Denied command", "agent", privateID)
	var posted CommentResponse
	testutil.Call(t, testHandler.CreateComment, withURLParam(newRequest(http.MethodPost, "/comments", map[string]any{"content": "!sop-remote inspect {}"}), "id", issue)).Want(http.StatusCreated).JSON(&posted)
	if len(posted.TriggerOutcomes) != 1 || posted.TriggerOutcomes[0].ReasonCode != ReasonInvocationNotAllowed {
		t.Fatalf("outcome=%+v", posted.TriggerOutcomes)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, issue); n != 0 {
		t.Fatal("unauthorized command was queued")
	}
}
