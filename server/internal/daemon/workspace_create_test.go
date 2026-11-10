package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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
			request := httptest.NewRequest(http.MethodPost, "/api/workspaces", strings.NewReader(`{"name":"New","slug":"new"}`))
			request.Header.Set("Authorization", "Bearer mat_task")
			response := httptest.NewRecorder()
			d.workspaceHandler().ServeHTTP(response, request)
			if response.Code != http.StatusCreated || !created || strings.Contains(response.Body.String(), "mul_runtime") {
				t.Fatalf("response=%d %s, created=%v", response.Code, response.Body.String(), created)
			}

			d.clearActiveRepoCheckoutTask("mat_task")
			created = false
			response = httptest.NewRecorder()
			d.workspaceHandler().ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || created {
				t.Fatalf("inactive task response=%d %s, created=%v", response.Code, response.Body.String(), created)
			}
		})
	}
}

func TestWorkspaceOperationsUseRuntimePAT(t *testing.T) {
	const base = "/api/workspaces/33333333-3333-3333-3333-333333333333"
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/workspaces", "", 200},
		{"GET", base, "", 200},
		{"GET", base + "/package?category=skills&limit=2&offset=1", "", 200},
		{"GET", base + "/package/default", "", 200},
		{"POST", base + "/export", `{"projects":["selected"],"issues":["one"]}`, 200},
		{"POST", base + "/import/inspect?category=skills&limit=2", `{"package":{},"selection":{}}`, 200},
		{"POST", base + "/import", `{"package":{}}`, 200},
		{"POST", base + "/import", `{"package":{},"activate_plugins":true}`, 200},
		{"POST", base + "/import", `{"package":{},"activate_plugins":true}`, 403},
	} {
		t.Run(tc.method+" "+tc.path+tc.body+fmt.Sprint(tc.status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					if r.URL.Path != "/api/agents/worker" || r.Header.Get("Authorization") != "Bearer mat_task" {
						t.Errorf("task must be validated first: %s", r.URL.Path)
					}
					fmt.Fprint(w, `{}`)
					return
				}
				if r.Header.Get("Authorization") != "Bearer mul_runtime" || r.Method != tc.method || r.URL.RequestURI() != tc.path {
					t.Errorf("unexpected runtime request: %s %s", r.Method, r.URL.RequestURI())
				}
				if tc.method == "POST" {
					body, _ := io.ReadAll(r.Body)
					want := strings.ReplaceAll(tc.body, `"activate_plugins":true`, `"plugin_execution_token":"mul_runtime"`)
					var gotJSON, wantJSON any
					json.Unmarshal(body, &gotJSON)
					json.Unmarshal([]byte(want), &wantJSON)
					if !reflect.DeepEqual(gotJSON, wantJSON) {
						t.Errorf("body=%s, want %s", body, want)
					}
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"echo":"mul_runtime"}`)
			}))
			defer server.Close()
			d := &Daemon{client: NewClient(server.URL)}
			d.client.SetToken("mul_runtime")
			d.registerActiveRepoCheckoutTask("mat_task", activeRepoCheckoutTask{AgentID: "worker"})
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer mat_task")
			w := httptest.NewRecorder()
			d.workspaceHandler().ServeHTTP(w, r)
			if calls != 2 || w.Code != tc.status || strings.Contains(w.Body.String(), "mul_runtime") {
				t.Fatalf("calls=%d, response=%d %s", calls, w.Code, w.Body.String())
			}
			d.clearActiveRepoCheckoutTask("mat_task")
			w = httptest.NewRecorder()
			d.workspaceHandler().ServeHTTP(w, r)
			if calls != 2 || w.Code != http.StatusUnauthorized {
				t.Fatalf("inactive task reached backend: calls=%d status=%d", calls, w.Code)
			}
		})
	}
}

func TestWorkspaceProxyRejectsOtherOperations(t *testing.T) {
	const base = "/api/workspaces/33333333-3333-3333-3333-333333333333"
	for _, tc := range []struct{ method, path string }{
		{"DELETE", base}, {"PATCH", base}, {"POST", base + "/members"},
		{"GET", base + "/members"}, {"GET", "/api/tokens"},
		{"POST", base + "/package"}, {"GET", base + "/export"},
		{"POST", "/api/workspaces/../tokens"}, {"GET", base + "/../members"},
	} {
		w := httptest.NewRecorder()
		(&Daemon{}).workspaceHandler().ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: status=%d", tc.method, tc.path, w.Code)
		}
	}
}

func TestWorkspaceProxyRequiresValidTaskAndRuntimePAT(t *testing.T) {
	for _, tc := range []struct {
		name, pat          string
		authStatus, status int
	}{
		{"revoked task", "mul_runtime", 401, 502},
		{"missing runtime PAT", "", 200, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/agents/worker" || r.Header.Get("Authorization") != "Bearer mat_task" {
					t.Errorf("unauthorized request spent the runtime PAT: %s", r.URL.Path)
				}
				w.WriteHeader(tc.authStatus)
				fmt.Fprint(w, `{}`)
			}))
			defer server.Close()
			d := &Daemon{client: NewClient(server.URL)}
			d.client.SetToken(tc.pat)
			d.registerActiveRepoCheckoutTask("mat_task", activeRepoCheckoutTask{AgentID: "worker"})
			r := httptest.NewRequest("GET", "/api/workspaces", nil)
			r.Header.Set("Authorization", "Bearer mat_task")
			w := httptest.NewRecorder()
			d.workspaceHandler().ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d, want %d", w.Code, tc.status)
			}
		})
	}
}
