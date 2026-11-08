package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) TakeRemoteSOPs(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	requests, err := h.takeRemoteSOPs(r.Context(), runtime)
	if err != nil {
		writeError(w, 500, "failed to take remote SOP requests")
		return
	}
	writeJSON(w, http.StatusOK, requests)
}

func (h *Handler) takeRemoteSOPs(ctx context.Context, runtime db.AgentRuntime) ([]protocol.RemoteSOPRequest, error) {
	comments, err := h.Queries.ListRemoteSOPComments(ctx, runtime.ID)
	if err != nil {
		return nil, err
	}
	requests := []protocol.RemoteSOPRequest{}
	for _, comment := range comments {
		issue, err := h.Queries.GetIssue(ctx, comment.IssueID)
		if err != nil {
			return nil, err
		}
		skills, err := h.TaskService.LoadAgentSkills(ctx, issue.AssigneeID)
		if err != nil {
			return nil, err
		}
		skillJSON, err := json.Marshal(skills)
		if err != nil {
			return nil, err
		}
		prior, err := h.Queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{AgentID: issue.AssigneeID, IssueID: issue.ID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		token, err := auth.GenerateAgentTaskToken()
		if err != nil {
			return nil, err
		}
		tx, err := h.TxStarter.Begin(ctx)
		if err != nil {
			return nil, err
		}
		request, err := func() (protocol.RemoteSOPRequest, error) {
			defer tx.Rollback(ctx)
			q := h.Queries.WithTx(tx)
			if _, err := q.TakeRemoteSOPComment(ctx, db.TakeRemoteSOPCommentParams{ID: comment.ID, WorkspaceID: comment.WorkspaceID}); err != nil {
				return protocol.RemoteSOPRequest{}, err
			}
			// This completed row stores execution output; it never enters the agent queue.
			if _, err := q.CreateRemoteSOPLog(ctx, db.CreateRemoteSOPLogParams{ID: comment.ID, AgentID: issue.AssigneeID, RuntimeID: runtime.ID, IssueID: issue.ID}); err != nil {
				return protocol.RemoteSOPRequest{}, err
			}
			if _, err := q.CreateTaskMessage(ctx, db.CreateTaskMessageParams{ID: dbid.NewV7(), TaskID: comment.ID, Seq: 1, Type: "tool_use", Tool: pgtype.Text{String: "remote-sop", Valid: true}, Content: pgtype.Text{String: comment.Content, Valid: true}, CallID: pgtype.Text{String: uuidToString(comment.ID), Valid: true}}); err != nil {
				return protocol.RemoteSOPRequest{}, err
			}
			if _, err := q.CreateTaskToken(ctx, db.CreateTaskTokenParams{ID: dbid.NewV7(), TokenHash: auth.HashToken(token), TaskID: comment.ID, AgentID: issue.AssigneeID, WorkspaceID: issue.WorkspaceID, UserID: runtime.OwnerID, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true}}); err != nil {
				return protocol.RemoteSOPRequest{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return protocol.RemoteSOPRequest{}, err
			}
			return protocol.RemoteSOPRequest{ID: uuidToString(comment.ID), AgentID: uuidToString(issue.AssigneeID), IssueID: uuidToString(issue.ID), WorkspaceID: uuidToString(issue.WorkspaceID), Content: comment.Content, WorkDir: prior.WorkDir.String, Skills: skillJSON, AuthToken: token}, nil
		}()
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		h.publish(protocol.EventCommentDeleted, request.WorkspaceID, "agent", request.AgentID, map[string]any{"comment_id": request.ID, "issue_id": request.IssueID})
		requests = append(requests, request)
	}
	return requests, nil
}
