package daemon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkspaceCreationUsesRuntimePAT(t *testing.T) {
	for _, agentID := range []string{"mika", "worker"} {
		t.Run(agentID, func(t *testing.T) {
			created := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET /api/agents/" + agentID:
					if r.Header.Get("Authorization") != "Bearer mat_task" {
						t.Error("identity lookup needs the task credential")
					}
					fmt.Fprint(w, `{}`)
				case "POST /api/workspaces":
					if r.Header.Get("Authorization") != "Bearer mul_runtime" {
						t.Error("workspace creation needs the runtime PAT")
					}
					created = true
					w.WriteHeader(http.StatusCreated)
					fmt.Fprint(w, `{"id":"new-workspace"}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()
			d := &Daemon{client: NewClient(server.URL)}
			d.client.SetToken("mul_runtime")
			d.registerActiveRepoCheckoutTask("mat_task", activeRepoCheckoutTask{AgentID: agentID})
			request := httptest.NewRequest(http.MethodPost, "/workspace/create", strings.NewReader(`{"name":"New","slug":"new"}`))
			request.Header.Set("Authorization", "Bearer mat_task")
			response := httptest.NewRecorder()
			d.workspaceCreateHandler().ServeHTTP(response, request)
			if response.Code != http.StatusCreated || !created || strings.Contains(response.Body.String(), "mul_runtime") {
				t.Fatalf("response=%d %s, created=%v", response.Code, response.Body.String(), created)
			}

			d.clearActiveRepoCheckoutTask("mat_task")
			created = false
			response = httptest.NewRecorder()
			d.workspaceCreateHandler().ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || created {
				t.Fatalf("inactive task response=%d %s, created=%v", response.Code, response.Body.String(), created)
			}
		})
	}
}
