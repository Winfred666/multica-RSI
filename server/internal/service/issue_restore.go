package service

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueRestoreData contains validated content with newly allocated IDs.
// Ownership is assigned by Create, never taken from the exported document.
type IssueRestoreData struct {
	Metadata           map[string]json.RawMessage
	Comments           []db.CreateCommentParams
	DeletedCommentIDs  []pgtype.UUID
	ResolvedCommentIDs []pgtype.UUID
	Attachments        []db.CreateAttachmentParams
}

func persistIssueRestore(ctx context.Context, q *db.Queries, issue db.Issue, data IssueRestoreData) error {
	for key, value := range data.Metadata {
		if _, err := q.SetIssueMetadataKey(ctx, db.SetIssueMetadataKeyParams{
			ID: issue.ID, WorkspaceID: issue.WorkspaceID, Key: key, Value: value,
		}); err != nil {
			return err
		}
	}
	for _, c := range data.Comments {
		c.IssueID, c.WorkspaceID = issue.ID, issue.WorkspaceID
		c.AuthorType, c.AuthorID = issue.CreatorType, issue.CreatorID
		if _, err := q.CreateComment(ctx, c); err != nil {
			return err
		}
	}
	for _, id := range data.DeletedCommentIDs {
		if _, err := q.TombstoneComment(ctx, db.TombstoneCommentParams{ID: id, WorkspaceID: issue.WorkspaceID}); err != nil {
			return err
		}
	}
	for _, id := range data.ResolvedCommentIDs {
		if _, err := q.ResolveComment(ctx, db.ResolveCommentParams{
			ID:             id,
			ResolvedByType: pgtype.Text{String: issue.CreatorType, Valid: true}, ResolvedByID: issue.CreatorID,
		}); err != nil {
			return err
		}
	}
	for _, a := range data.Attachments {
		a.IssueID, a.WorkspaceID = issue.ID, issue.WorkspaceID
		a.UploaderType, a.UploaderID = issue.CreatorType, issue.CreatorID
		if _, err := q.CreateAttachment(ctx, a); err != nil {
			return err
		}
	}
	return nil
}
