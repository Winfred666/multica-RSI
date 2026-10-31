package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

const (
	maxIssueSnapshotBytes       = 16 << 20
	maxIssueSnapshotFileBytes   = 8 << 20
	maxIssueSnapshotComments    = 2000
	maxIssueSnapshotAttachments = 100
)

// IssueSnapshot is caller-owned JSON, not a persisted backend snapshot.
// Original identities and timestamps are provenance; restore allocates new ones.
type IssueSnapshot struct {
	Version     int                       `json:"version"`
	CapturedAt  string                    `json:"captured_at"`
	Issue       IssueResponse             `json:"issue"`
	Comments    []CommentResponse         `json:"comments"`
	Attachments []IssueSnapshotAttachment `json:"attachments"`
}

type IssueSnapshotAttachment struct {
	AttachmentResponse
	Data []byte `json:"data"`
}

type RestoreIssueRequest struct {
	Snapshot  IssueSnapshot `json:"snapshot"`
	Title     *string       `json:"title,omitempty"`
	ProjectID *string       `json:"project_id,omitempty"`
}

func (h *Handler) GetIssueSnapshot(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	snapshot, err := h.collectIssueSnapshot(r.Context(), issue)
	if err != nil {
		slog.Warn("collect issue snapshot failed", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusConflict, "could not collect a complete snapshot")
		return
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode snapshot")
		return
	}
	if len(data) > maxIssueSnapshotBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "snapshot exceeds 16 MiB")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handler) collectIssueSnapshot(ctx context.Context, source db.Issue) (IssueSnapshot, error) {
	snapshot := IssueSnapshot{Version: 1, CapturedAt: time.Now().UTC().Format(time.RFC3339Nano), Comments: []CommentResponse{}, Attachments: []IssueSnapshotAttachment{}}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return snapshot, err
	}
	defer tx.Rollback(ctx)
	// All database reads see the same issue revision and comment/attachment set.
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
		return snapshot, err
	}
	q := h.Queries.WithTx(tx)
	issue, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: source.ID, WorkspaceID: source.WorkspaceID})
	if err != nil {
		return snapshot, err
	}
	snapshot.Issue = issueToResponse(issue, h.getIssuePrefix(ctx, issue.WorkspaceID))
	labels, err := q.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return snapshot, err
	}
	labelResponses := labelsToResponse(labels)
	snapshot.Issue.Labels = &labelResponses
	comments, err := q.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, Limit: maxIssueSnapshotComments + 1})
	if err != nil {
		return snapshot, err
	}
	if len(comments) > maxIssueSnapshotComments {
		return snapshot, fmt.Errorf("snapshot exceeds %d comments", maxIssueSnapshotComments)
	}
	commentIDs := make([]pgtype.UUID, 0, len(comments))
	for _, c := range comments {
		snapshot.Comments = append(snapshot.Comments, commentToResponse(c, nil, nil))
		commentIDs = append(commentIDs, c.ID)
	}
	attachments, err := q.ListAttachmentsByIssue(ctx, db.ListAttachmentsByIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return snapshot, err
	}
	commentAttachments, err := q.ListAttachmentsByCommentIDs(ctx, db.ListAttachmentsByCommentIDsParams{Column1: commentIDs, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return snapshot, err
	}
	seen := map[pgtype.UUID]bool{}
	remaining := int64(maxIssueSnapshotFileBytes)
	for _, a := range append(attachments, commentAttachments...) {
		if seen[a.ID] || a.SourceContextID.Valid || a.ChatSessionID.Valid {
			continue
		}
		seen[a.ID] = true
		if len(seen) > maxIssueSnapshotAttachments {
			return snapshot, fmt.Errorf("snapshot exceeds %d attachments", maxIssueSnapshotAttachments)
		}
		if h.Storage == nil {
			return snapshot, errors.New("attachment storage is unavailable")
		}
		reader, err := h.Storage.GetReader(ctx, h.Storage.KeyFromURL(a.Url))
		if err != nil {
			return snapshot, errors.New("attachment could not be read")
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, remaining+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			return snapshot, errors.New("attachment could not be read")
		}
		remaining -= int64(len(data))
		if remaining < 0 {
			return snapshot, errors.New("snapshot attachments exceed 8 MiB")
		}
		snapshot.Attachments = append(snapshot.Attachments, IssueSnapshotAttachment{AttachmentResponse: h.attachmentToResponse(a, attachmentURLModeStable), Data: data})
	}
	return snapshot, tx.Commit(ctx)
}

func (h *Handler) RestoreIssue(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	ws, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}
	// Leave room for the restore envelope around a maximum-sized snapshot.
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxIssueSnapshotBytes+(1<<20)))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "restore request exceeds 17 MiB")
		return
	}
	var req RestoreIssueRequest
	if err := json.Unmarshal(data, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid snapshot request")
		return
	}
	if req.Snapshot.Version != 1 {
		writeError(w, http.StatusBadRequest, "unsupported snapshot version")
		return
	}
	if req.Snapshot.Issue.WorkspaceID != workspaceID {
		writeError(w, http.StatusBadRequest, "snapshot must belong to this workspace")
		return
	}
	original := req.Snapshot.Issue
	if req.Title != nil {
		original.Title = *req.Title
	}
	if req.ProjectID != nil {
		original.ProjectID = req.ProjectID
	}
	if strings.TrimSpace(original.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if !validateIssueEnum(w, "priority", original.Priority, validIssuePriorities) {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	params := service.IssueCreateParams{WorkspaceID: ws, Title: sanitizeNullBytes(original.Title), Description: ptrToText(original.Description), Status: "backlog", Priority: original.Priority, CreatorType: actorType, CreatorID: parseUUID(actorID), AllowDuplicate: true}
	if original.ProjectID != nil && *original.ProjectID != "" {
		params.ProjectID, ok = parseUUIDOrBadRequest(w, *original.ProjectID, "project_id")
		if !ok {
			return
		}
	}
	for _, date := range []struct {
		value  *string
		target *pgtype.Date
	}{{original.StartDate, &params.StartDate}, {original.DueDate, &params.DueDate}} {
		if date.value != nil {
			parsed, err := util.ParseCalendarDate(*date.value)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid snapshot date")
				return
			}
			*date.target = parsed
		}
	}
	if original.Labels != nil {
		for _, label := range *original.Labels {
			id, valid := parseUUIDOrBadRequest(w, label.ID, "label_id")
			if !valid {
				return
			}
			params.LabelIDs = append(params.LabelIDs, id)
		}
	}
	props := map[string]json.RawMessage{}
	for key, value := range original.Properties {
		props[key], _ = json.Marshal(value)
	}
	params.Properties, ok = parseIssueCreateProperties(w, props)
	if !ok {
		return
	}
	restored, err := prepareIssueRestore(req.Snapshot)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	params.Restore = &restored
	if len(req.Snapshot.Attachments) > 0 && h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "attachment storage is unavailable")
		return
	}
	uploaded := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, key := range uploaded {
				h.Storage.Delete(context.Background(), key)
			}
		}
	}()
	replacements := []string{}
	for i, a := range req.Snapshot.Attachments {
		target := &restored.Attachments[i]
		id := uuidToString(target.ID)
		key := "workspaces/" + workspaceID + "/" + id + path.Ext(target.Filename)
		uploaded = append(uploaded, key)
		target.Url, err = h.Storage.Upload(r.Context(), key, a.Data, target.ContentType, target.Filename)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to restore attachment")
			return
		}
		url := h.buildMarkdownURL(db.Attachment{ID: target.ID, Url: target.Url}, id)
		for _, old := range []string{a.MarkdownURL, a.URL, util.AttachmentDownloadPath(a.ID)} {
			if old != "" {
				replacements = append(replacements, old, url)
			}
		}
	}
	replacer := strings.NewReplacer(replacements...)
	if params.Description.Valid {
		params.Description.String = sanitizeNullBytes(replacer.Replace(params.Description.String))
	}
	for i := range restored.Comments {
		restored.Comments[i].Content = replacer.Replace(restored.Comments[i].Content)
	}
	prefix := h.getIssuePrefix(r.Context(), ws)
	result, err := h.IssueService.Create(r.Context(), params, service.IssueCreateOpts{
		BroadcastPayload: func(issue db.Issue, _ []db.Attachment, labels []db.IssueLabel) map[string]any {
			resp := issueToResponse(issue, prefix)
			labelResponses := labelsToResponse(labels)
			resp.Labels = &labelResponses
			return map[string]any{"issue": resp}
		},
	})
	if errors.Is(err, service.ErrProjectNotFound) || errors.Is(err, service.ErrIssueLabelNotFound) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if writeIssueCreatePropertyError(w, err) || writeIssueLimitReached(w, err) {
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "issue_metadata_size_limit" {
		writeError(w, http.StatusBadRequest, "metadata exceeds 8 KiB")
		return
	}
	if err != nil {
		slog.Warn("restore issue failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to restore issue")
		return
	}
	committed = true
	resp := issueToResponse(result.Issue, prefix)
	labels := labelsToResponse(result.Labels)
	resp.Labels = &labels
	h.fillStatusCategory(r.Context(), ws, &resp)
	writeJSON(w, http.StatusCreated, resp)
}

func prepareIssueRestore(snapshot IssueSnapshot) (service.IssueRestoreData, error) {
	data := service.IssueRestoreData{Metadata: map[string]json.RawMessage{}}
	if len(snapshot.Comments) > maxIssueSnapshotComments || len(snapshot.Attachments) > maxIssueSnapshotAttachments {
		return data, errors.New("snapshot has too many comments or attachments")
	}
	if len(snapshot.Issue.Metadata) > maxIssueMetadataKeys {
		return data, errors.New("snapshot has too many metadata keys")
	}
	for key, value := range snapshot.Issue.Metadata {
		if err := validateIssueMetadataKey(key); err != nil {
			return data, err
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return data, err
		}
		if err := validateIssueMetadataValue(raw); err != nil {
			return data, err
		}
		data.Metadata[key] = raw
	}
	ids := map[string]pgtype.UUID{}
	parents := map[string]string{}
	deleted := map[string]bool{}
	for _, c := range snapshot.Comments {
		if _, err := util.ParseUUID(c.ID); err != nil {
			return data, errors.New("invalid comment id")
		}
		if _, exists := ids[c.ID]; exists {
			return data, errors.New("duplicate comment id")
		}
		ids[c.ID] = dbid.NewV7()
		if c.ParentID != nil {
			parents[c.ID] = *c.ParentID
		}
		deleted[c.ID] = c.DeletedAt != nil
	}
	for _, c := range snapshot.Comments {
		seen := map[string]bool{c.ID: true}
		for parent := parents[c.ID]; parent != ""; parent = parents[parent] {
			if !ids[parent].Valid || seen[parent] {
				return data, errors.New("comment parent is missing or cyclic")
			}
			seen[parent] = true
		}
		kind := c.Type
		if _, allowed := clientAuthorableCommentTypes[kind]; !allowed {
			kind = "comment"
		}
		content := sanitizeNullBytes(c.Content)
		if deleted[c.ID] {
			content = ""
		}
		data.Comments = append(data.Comments, db.CreateCommentParams{ID: ids[c.ID], ParentID: ids[parents[c.ID]], Content: content, Type: kind})
		if deleted[c.ID] {
			data.DeletedCommentIDs = append(data.DeletedCommentIDs, ids[c.ID])
		} else if c.ResolvedAt != nil {
			data.ResolvedCommentIDs = append(data.ResolvedCommentIDs, ids[c.ID])
		}
	}
	attachments := map[string]bool{}
	total := 0
	for _, a := range snapshot.Attachments {
		if _, err := util.ParseUUID(a.ID); err != nil {
			return data, errors.New("invalid attachment id")
		}
		if a.Data == nil {
			return data, errors.New("attachment data is required")
		}
		if attachments[a.ID] {
			return data, errors.New("duplicate attachment id")
		}
		attachments[a.ID] = true
		total += len(a.Data)
		if total > maxIssueSnapshotFileBytes {
			return data, errors.New("snapshot attachments exceed 8 MiB")
		}
		var parent pgtype.UUID
		if a.CommentID != nil {
			parent = ids[*a.CommentID]
			if !parent.Valid || deleted[*a.CommentID] {
				return data, errors.New("attachment comment is missing or deleted")
			}
		}
		filename := path.Base(strings.ReplaceAll(a.Filename, `\`, "/"))
		if filename == "." || filename == "/" || strings.ContainsAny(filename, "\x00\r\n") {
			return data, errors.New("invalid attachment filename")
		}
		contentType := http.DetectContentType(a.Data)
		if ct, exists := extContentTypes[strings.ToLower(path.Ext(filename))]; exists {
			contentType = ct
		}
		data.Attachments = append(data.Attachments, db.CreateAttachmentParams{ID: dbid.NewV7(), CommentID: parent, Filename: filename, ContentType: contentType, SizeBytes: int64(len(a.Data))})
	}
	return data, nil
}
