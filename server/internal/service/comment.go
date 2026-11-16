package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

type CommentDispatchStatus string

type CommentTriggerOutcome struct {
	TargetType string                `json:"target_type"`
	TargetID   string                `json:"target_id"`
	Status     CommentDispatchStatus `json:"status"`
	ReasonCode dispatch.ReasonCode   `json:"reason_code"`
}

type CommentOptions struct {
	AttachmentIDs        []pgtype.UUID
	OriginatorUserID     string
	SteerTaskIDs         []pgtype.UUID
	Steer                bool
	StableAttachmentURLs bool
}

type MissingCommentAttachmentError struct{ ID pgtype.UUID }

func (e *MissingCommentAttachmentError) Error() string {
	return fmt.Sprintf("attachment %s is no longer available", util.UUIDToString(e.ID))
}

// CreateComment is the common write and post-commit lifecycle for API comments
// and task replies. Request authentication/validation stays at the API boundary.
func (s *TaskService) CreateComment(ctx context.Context, issue db.Issue, params db.CreateCommentParams, opts CommentOptions) (db.CreateCommentRow, []CommentTriggerOutcome, error) {
	var created db.CreateCommentRow
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return created, nil, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `SELECT set_config('multica.actor_type',$1,true),set_config('multica.actor_id',$2,true),set_config('multica.source_task_id',$3,true)`, params.AuthorType, util.UUIDToString(params.AuthorID), util.UUIDToString(params.SourceTaskID))
	if err != nil {
		return created, nil, err
	}
	params.ID, params.IssueID, params.WorkspaceID = dbid.NewV7(), issue.ID, issue.WorkspaceID
	qtx := s.Queries.WithTx(tx)
	created, err = qtx.CreateComment(ctx, params)
	if err != nil {
		return created, nil, err
	}
	// Owner first, then attachments: keep the comment and its files one atomic
	// write, using the same issue -> comment -> attachment lock order as deletion.
	if len(opts.AttachmentIDs) > 0 {
		locked, err := qtx.LockAttachmentsForCommentLink(ctx, db.LockAttachmentsForCommentLinkParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, AttachmentIds: opts.AttachmentIDs})
		if err != nil {
			return created, nil, err
		}
		have := make(map[pgtype.UUID]bool, len(locked))
		for _, id := range locked {
			have[id] = true
		}
		for _, id := range opts.AttachmentIDs {
			if !have[id] {
				return created, nil, &MissingCommentAttachmentError{ID: id}
			}
		}
		if err := qtx.LinkAttachmentsToComment(ctx, db.LinkAttachmentsToCommentParams{CommentID: created.ID, IssueID: issue.ID, Column3: opts.AttachmentIDs}); err != nil {
			return created, nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return created, nil, err
	}
	if s.CommentCreated != nil {
		return created, s.CommentCreated(ctx, issue, created, opts), nil
	}
	payload := commentEventFields(created.Comment())
	payload["revision"] = created.Revision
	s.PublishCommentCreated(ctx, issue, created, payload)
	return created, nil, nil
}
