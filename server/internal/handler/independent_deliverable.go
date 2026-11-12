package handler

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/imagemetadata"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) replaceIndependentAttachment(w http.ResponseWriter, r *http.Request, userID, workspaceID, attachmentID, filename, contentType string, data []byte) {
	if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}
	id, ok := parseUUIDOrBadRequest(w, attachmentID, "attachment_id")
	if !ok {
		return
	}
	att, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{ID: id, WorkspaceID: parseUUID(workspaceID)})
	if err != nil || !att.Independent {
		writeError(w, http.StatusNotFound, "independent resource not found")
		return
	}
	var updated db.UpdateAttachmentRow
	err = h.withAttachmentOwnerLock(r.Context(), att, func(q *db.Queries) error {
		// Overwrite the current key; never create a previous-content object.
		link, err := h.Storage.Upload(r.Context(), h.Storage.KeyFromURL(att.Url), data, contentType, filename)
		if err != nil {
			return err
		}
		params := db.UpdateAttachmentParams{
			ID: id, WorkspaceID: att.WorkspaceID, Filename: filename, Url: link,
			ContentType: contentType, SizeBytes: int64(len(data)),
		}
		if dimensions, ok := imagemetadata.Dimensions(data); ok {
			params.Width = pgtype.Int4{Int32: dimensions.Width, Valid: true}
			params.Height = pgtype.Int4{Int32: dimensions.Height, Valid: true}
		}
		updated, err = q.UpdateAttachment(r.Context(), params)
		return err
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to replace independent resource")
		return
	}
	writeJSON(w, http.StatusOK, h.attachmentToResponse(db.CreateAttachmentRow(updated).Attachment(), attachmentURLModeFromRequest(r)))
}

func (h *Handler) validateDeliverablesMetadata(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, raw json.RawMessage) bool {
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		writeError(w, http.StatusBadRequest, "deliverables must be a string containing a JSON array of independent UUIDs")
		return false
	}
	var ids []string
	if err := json.Unmarshal([]byte(encoded), &ids); err != nil || ids == nil {
		writeError(w, http.StatusBadRequest, "deliverables must encode a JSON array of independent UUIDs")
		return false
	}
	parsed := make([]pgtype.UUID, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		value, ok := parseUUIDOrBadRequest(w, id, "deliverables UUID")
		if !ok {
			return false
		}
		if uuidToString(value) != id || seen[id] {
			writeError(w, http.StatusBadRequest, "deliverables must contain distinct canonical UUIDs")
			return false
		}
		seen[id] = true
		parsed = append(parsed, value)
	}
	attachments, err := h.Queries.ListAttachmentsByIDs(r.Context(), db.ListAttachmentsByIDsParams{AttachmentIds: parsed, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate deliverables")
		return false
	}
	if len(attachments) != len(parsed) {
		writeError(w, http.StatusBadRequest, "deliverables must reference independent resources in this workspace")
		return false
	}
	for _, a := range attachments {
		if !a.Independent {
			writeError(w, http.StatusBadRequest, "deliverables cannot reference ordinary attachments")
			return false
		}
	}
	return true
}
