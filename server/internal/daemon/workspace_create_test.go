package daemon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkspaceCreationUsesRuntimePAT(t *testing.T) {
	for _, systemKey := range []string{"mika", ""} {
		t.Run(systemKey, func(t *testing.T) {
			created := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET /api/agents/agent-1":
					if r.Header.Get("Authorization") != "Bearer mat_task" {
						t.Error("identity lookup needs the task credential")
					}
					fmt.Fprintf(w, `{"system_key":%q}`, systemKey)
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
			d.registerActiveRepoCheckoutTask("mat_task", activeRepoCheckoutTask{AgentID: "agent-1"})
			request := httptest.NewRequest(http.MethodPost, "/workspace/create", strings.NewReader(`{"name":"New","slug":"new"}`))
			request.Header.Set("Authorization", "Bearer mat_task")
			response := httptest.NewRecorder()
			d.workspaceCreateHandler().ServeHTTP(response, request)
			want := http.StatusForbidden
			if systemKey == "mika" {
				want = http.StatusCreated
			}
			if response.Code != want || created != (systemKey == "mika") || strings.Contains(response.Body.String(), "mul_runtime") {
				t.Fatalf("response=%d %s, created=%v", response.Code, response.Body.String(), created)
			}
		})
	}
}
