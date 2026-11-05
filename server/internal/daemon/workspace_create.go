package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func (d *Daemon) workspaceCreateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		task, result := d.activeRepoCheckoutTask(r)
		if result != repoCheckoutAuthOK {
			http.Error(w, "active task credential required", http.StatusUnauthorized)
			return
		}
		var agent struct {
			SystemKey string `json:"system_key"`
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if err := d.client.getJSONWithToken(r.Context(), "/api/agents/"+task.AgentID, token, &agent); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if agent.SystemKey != "mika" {
			http.Error(w, "workspace creation through the runtime requires Mika", http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(d.client.Token(), "mul_") {
			http.Error(w, "the runtime needs a personal access token to create a workspace", http.StatusServiceUnavailable)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		var workspace json.RawMessage
		if err := d.client.postJSON(r.Context(), "/api/workspaces", body, &workspace); err != nil {
			status := http.StatusBadGateway
			var response *requestError
			if errors.As(err, &response) {
				status = response.StatusCode
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(workspace)
	}
}
