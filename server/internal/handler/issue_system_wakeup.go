package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Installed clients may still read these routes. Automatic child wakeups now
// belong to the workspace plugin; ordinary user-created wakeups are unchanged.
func (h *Handler) ListIssueSystemWakeups(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id")); !ok {
		return
	}
	writeJSON(w, http.StatusOK, []any{})
}

func (h *Handler) UpdateIssueSystemWakeup(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id")); !ok {
		return
	}
	writeError(w, http.StatusGone, "configure Wakeup for sub-issue in Settings > Plugins")
}

func (h *Handler) ListWorkspaceSystemWakeups(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r)); !ok {
		return
	}
	writeJSON(w, http.StatusOK, []any{})
}

func (h *Handler) UpdateWorkspaceSystemWakeup(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r)); !ok {
		return
	}
	writeError(w, http.StatusGone, "configure Wakeup for sub-issue in Settings > Plugins")
}
