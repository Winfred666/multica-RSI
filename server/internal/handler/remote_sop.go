package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ConsumeRemoteSOP removes an execution-log request without cancelling its task.
func (h *Handler) ConsumeRemoteSOP(w http.ResponseWriter, r *http.Request) {
	task, scope, ok := h.requireDaemonTaskAccessWithScope(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	commentID, ok := parseUUIDOrBadRequest(w, r.URL.Query().Get("comment_id"), "comment id")
	if !ok {
		return
	}
	comment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{ID: commentID, WorkspaceID: parseUUID(scope.WorkspaceID)})
	if err != nil {
		writeError(w, 404, "comment not found")
		return
	}
	issue, err := h.Queries.GetIssue(r.Context(), comment.IssueID)
	if err != nil {
		writeError(w, 500, "failed to load issue")
		return
	}
	if comment.IssueID != task.IssueID || issue.AssigneeType.String != "agent" || issue.AssigneeID != task.AgentID || !strings.HasPrefix(comment.Content, "!sop ") {
		writeError(w, 400, "remote SOP must target this issue's assignee")
		return
	}
	// A remote request is consumed by the daemon rather than delivered to a model.
	_, err = h.Queries.AckTaskSupplementDelivered(r.Context(), db.AckTaskSupplementDeliveredParams{TaskID: task.ID, CommentID: commentID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 500, "failed to acknowledge remote SOP")
		return
	}
	deleted, err := h.deleteComment(r.Context(), commentID, comment.WorkspaceID)
	if err != nil {
		writeError(w, 500, "failed to consume remote SOP comment")
		return
	}
	h.deleteS3Objects(r.Context(), deleted.AttachmentURLs)
	if deleted.Tombstone != nil {
		resp := commentToResponse(*deleted.Tombstone, nil, nil)
		resp.IssueRevision = deleted.IssueRevision
		h.publish(protocol.EventCommentUpdated, scope.WorkspaceID, "agent", uuidToString(task.AgentID), map[string]any{"comment": resp, "issue_revision": deleted.IssueRevision})
	}
	for _, id := range deleted.RemovedIDs {
		h.publish(protocol.EventCommentDeleted, scope.WorkspaceID, "agent", uuidToString(task.AgentID), map[string]any{"comment_id": uuidToString(id), "issue_id": uuidToString(issue.ID), "issue_revision": deleted.IssueRevision})
	}
	w.WriteHeader(http.StatusNoContent)
}
