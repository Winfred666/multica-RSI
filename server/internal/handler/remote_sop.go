package handler

import (
	"context"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The marker selects command delivery. Its name, payload and arguments belong
// to the daemon/CLI, and the original comment remains the execution input.
// TODO: Stage 4: SoP as first-class member: Directly using special ACP AS command executor in the future inside daemon
func isRemoteSOPComment(content string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(content), " ")
	return first == "!sop-remote"
}

// TODO: Stage 4: SoP as first-class member: Directly using special ACP AS command executor in the future inside daemon
func (h *Handler) triggerRemoteSOP(ctx context.Context, issue db.Issue, comment db.Comment, actorType, actorID, originator string) []CommentTriggerOutcome {
	outcome := CommentTriggerOutcome{TargetType: "agent", TargetID: uuidToString(issue.AssigneeID), Status: DispatchBlocked, ReasonCode: ReasonTargetUnavailable}
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" {
		return []CommentTriggerOutcome{outcome}
	}
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: issue.AssigneeID, WorkspaceID: issue.WorkspaceID})
	if err == nil && h.canInvokeAgent(ctx, agent, actorType, actorID, originator, uuidToString(issue.WorkspaceID)) {
		if _, err := h.TaskService.EnqueueRemoteSOPTask(ctx, issue, comment.ID); err == nil {
			outcome.Status, outcome.ReasonCode = DispatchQueued, ReasonQueued
		} else {
			slog.Warn("remote SoP enqueue failed", "error", err, "comment_id", uuidToString(comment.ID))
			outcome.ReasonCode = ReasonInternalError
		}
	} else {
		outcome.ReasonCode = ReasonInvocationNotAllowed
	}
	return []CommentTriggerOutcome{outcome}
}

func (h *Handler) noteRemoteSOPCleanupFailure(ctx context.Context, task db.AgentTaskQueue) {
	// TODO: Stage 4: SoP as first-class member: Directly using special ACP AS command executor in the future inside daemon
	if !service.IsRemoteSOPTask(task) {
		return
	}

	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return
	}
	created, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID: dbid.NewV7(), IssueID: task.IssueID, WorkspaceID: issue.WorkspaceID,
		AuthorType: "system", AuthorID: pgtype.UUID{Valid: true}, Type: "system",
		Content: "Cannot close the questionnaire: no comment editing permission.",
	})
	if err != nil {
		slog.Warn("remote SoP cleanup notice failed", "error", err)
		return
	}
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"comment": commentToResponse(created.Comment(), nil, nil), "issue_revision": created.IssueRevision,
	})
}
