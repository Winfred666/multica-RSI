package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

func (d *Daemon) workspaceHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !workspaceRequestAllowed(r.Method, r.URL.Path) {
			http.Error(w, "workspace operation not allowed", http.StatusForbidden)
			return
		}
		task, result := d.activeRepoCheckoutTask(r)
		if result != repoCheckoutAuthOK {
			http.Error(w, "active task credential required", http.StatusUnauthorized)
			return
		}
		// Validate the active task credential before spending the runtime PAT.
		var agent struct{}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if err := d.client.getJSONWithToken(r.Context(), "/api/agents/"+task.AgentID, token, &agent); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		pat := d.client.Token()
		if !strings.HasPrefix(pat, "mul_") {
			http.Error(w, "the runtime needs a personal access token for workspace operations", http.StatusServiceUnavailable)
			return
		}
		path := r.URL.Path
		if r.URL.RawQuery != "" {
			path += "?" + r.URL.RawQuery
		}
		var response json.RawMessage
		var err error
		if r.Method == http.MethodGet {
			err = d.client.getJSON(r.Context(), path, &response)
		} else {
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body == nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/import") {
				var activate bool
				if raw, ok := body["activate_plugins"]; ok {
					if err := json.Unmarshal(raw, &activate); err != nil {
						http.Error(w, "activate_plugins must be a boolean", http.StatusBadRequest)
						return
					}
				}
				delete(body, "activate_plugins")
				delete(body, "plugin_execution_token")
				if activate {
					body["plugin_execution_token"], _ = json.Marshal(pat)
				}
			}
			err = d.client.postJSON(r.Context(), path, body, &response)
		}
		if err != nil {
			status := http.StatusBadGateway
			var response *requestError
			if errors.As(err, &response) {
				status = response.StatusCode
			}
			http.Error(w, strings.ReplaceAll(err.Error(), pat, "[REDACTED]"), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/workspaces" {
			w.WriteHeader(http.StatusCreated)
		}
		// Import errors and responses must never reveal the injected PAT.
		w.Write(bytes.ReplaceAll(response, []byte(pat), []byte("[REDACTED]")))
	}
}

func workspaceRequestAllowed(method, path string) bool {
	if path == "/api/workspaces" {
		return method == http.MethodGet || method == http.MethodPost
	}
	rest, ok := strings.CutPrefix(path, "/api/workspaces/")
	if !ok {
		return false
	}
	id, operation, _ := strings.Cut(rest, "/")
	if _, err := uuid.Parse(id); err != nil {
		return false
	}
	switch method {
	case http.MethodGet:
		return operation == "" || operation == "package" || operation == "package/default"
	case http.MethodPost:
		return operation == "export" || operation == "import" || operation == "import/inspect"
	}
	return false
}
