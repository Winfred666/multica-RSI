package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
	// TODO: need better assignment for remote calling comment in that issue
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
		callerAgentID, callerUserID := pgtype.UUID{}, comment.CallerUserID
		logAgentID := issue.AssigneeID
		if comment.AuthorType == "agent" {
			callerAgentID, logAgentID = comment.AuthorID, comment.AuthorID
		} else if !callerUserID.Valid {
			callerUserID = comment.AuthorID
		}
		if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: callerUserID, WorkspaceID: issue.WorkspaceID}); err != nil {
			return nil, err
		}
		workDir, err := h.Queries.GetRemoteSOPWorkDir(ctx, db.GetRemoteSOPWorkDirParams{RuntimeID: runtime.ID, AgentID: issue.AssigneeID, IssueID: issue.ID})
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
			// This completed audit log retains caller provenance/output, never queued.
			if _, err := q.CreateRemoteSOPLog(ctx, db.CreateRemoteSOPLogParams{ID: comment.ID, AgentID: logAgentID, RuntimeID: runtime.ID, IssueID: issue.ID, OriginatorUserID: callerUserID}); err != nil {
				return protocol.RemoteSOPRequest{}, err
			}
			if _, err := q.CreateTaskMessage(ctx, db.CreateTaskMessageParams{ID: dbid.NewV7(), TaskID: comment.ID, Seq: 1, Type: "tool_use", Tool: pgtype.Text{String: "remote-sop", Valid: true}, Content: pgtype.Text{String: comment.Content, Valid: true}, CallID: pgtype.Text{String: uuidToString(comment.ID), Valid: true}}); err != nil {
				return protocol.RemoteSOPRequest{}, err
			}
			if _, err := q.CreateTaskToken(ctx, db.CreateTaskTokenParams{ID: dbid.NewV7(), TokenHash: auth.HashToken(token), TaskID: comment.ID, AgentID: callerAgentID, WorkspaceID: issue.WorkspaceID, UserID: callerUserID, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true}}); err != nil {
				return protocol.RemoteSOPRequest{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return protocol.RemoteSOPRequest{}, err
			}
			return protocol.RemoteSOPRequest{ID: uuidToString(comment.ID), AgentID: uuidToString(issue.AssigneeID), IssueID: uuidToString(issue.ID), WorkspaceID: uuidToString(issue.WorkspaceID), Content: comment.Content, WorkDir: workDir.String, AuthToken: token, CallerType: comment.AuthorType, CallerID: uuidToString(comment.AuthorID)}, nil
		}()
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func (h *Handler) noteRemoteSOPCleanupFailure(ctx context.Context, task db.AgentTaskQueue) {
	var execution struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(task.Context, &execution) != nil || execution.Type != "remote_sop" {
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
