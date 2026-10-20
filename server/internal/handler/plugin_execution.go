package handler

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
)

func (h *Handler) pluginExecutionWorkspace(w http.ResponseWriter, r *http.Request, manage bool) (pgtype.UUID, bool) {
	if !h.requirePluginsV1(w, r) {
		return pgtype.UUID{}, false
	}
	workspaceID := workspaceIDFromURL(r, "id")
	id, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return pgtype.UUID{}, false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return pgtype.UUID{}, false
	}
	if manage && member.Role != "owner" && member.Role != "admin" {
		writeError(w, http.StatusForbidden, "workspace owner/admin access required")
		return pgtype.UUID{}, false
	}
	return id, true
}

func (h *Handler) GetPluginExecution(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.pluginExecutionWorkspace(w, r, false)
	if !ok {
		return
	}
	status, err := h.PluginService.PluginExecutionStatus(r.Context(), workspaceID)
	if err != nil {
		writePluginError(w, err, "failed to load hosted execution settings")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) SetPluginExecution(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.pluginExecutionWorkspace(w, r, true)
	if !ok {
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid credential request")
		return
	}
	status, err := h.PluginService.SetPluginExecution(r.Context(), workspaceID, req.Token)
	if err != nil {
		writePluginError(w, err, "failed to configure hosted execution")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) DeletePluginExecution(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.pluginExecutionWorkspace(w, r, true)
	if !ok {
		return
	}
	if err := h.PluginService.DeletePluginExecution(r.Context(), workspaceID); err != nil {
		writePluginError(w, err, "failed to disable hosted execution")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
