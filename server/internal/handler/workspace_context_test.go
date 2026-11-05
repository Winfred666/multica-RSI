package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/harness"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func workspaceHarnessServer(t *testing.T) {
	t.Helper()
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer harness-test" {
				t.Errorf("installer request missing authentication")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			r.Header.Set("X-User-ID", testUserID)
			next.ServeHTTP(w, r)
		})
	})
	router.Get("/api/skills", testHandler.ListSkills)
	router.Post("/api/skills", testHandler.CreateSkill)
	router.Put("/api/skills/{id}", testHandler.UpdateSkill)
	router.Patch("/api/workspaces/{id}", testHandler.UpdateWorkspace)
	server := httptest.NewServer(router)
	previous := testHandler.cfg.PublicURL
	testHandler.cfg.PublicURL = server.URL
	t.Cleanup(func() { testHandler.cfg.PublicURL = previous; server.Close() })
}

func workspaceHarnessRequest(body any) *http.Request {
	r := newRequest(http.MethodPost, "/api/workspaces", body)
	r.Header.Set("Authorization", "Bearer harness-test")
	return r
}

func cleanupWorkspaceHarness(t *testing.T, workspaceID string) {
	t.Helper()
	dbfx.Cleanup(t, `DELETE FROM skill WHERE workspace_id = $1`, workspaceID)
	dbfx.Cleanup(t, `DELETE FROM skill_file WHERE skill_id IN (SELECT id FROM skill WHERE workspace_id = $1)`, workspaceID)
}

func TestCreateWorkspace_InstallsStartupKit(t *testing.T) {
	workspaceHarnessServer(t)
	for _, name := range []string{"default", "custom"} {
		t.Run(name, func(t *testing.T) {
			prompt, err := harness.Files.ReadFile("startup.gotmpl")
			if err != nil {
				t.Fatal(err)
			}
			want := string(prompt)
			body := map[string]any{"name": "Startup prompt", "slug": "handler-tests-startup-" + name}
			if name == "custom" {
				want += "\nWorkspace-specific instructions."
				body["context"] = want
			}
			var created WorkspaceResponse
			testutil.Call(t, testHandler.CreateWorkspace, workspaceHarnessRequest(body)).Want(http.StatusCreated).JSON(&created)
			dbfx.Cleanup(t, `DELETE FROM workspace WHERE id = $1`, created.ID)
			dbfx.Cleanup(t, `DELETE FROM member WHERE workspace_id = $1`, created.ID)
			dbfx.Cleanup(t, `DELETE FROM issue_status WHERE workspace_id = $1`, created.ID)
			cleanupWorkspaceHarness(t, created.ID)
			if created.Context == nil || *created.Context != want {
				t.Fatalf("created context = %v, want supplied or bundled prompt", created.Context)
			}
			var systems, libraries int
			dbfx.QueryRow(t, `SELECT count(*) FILTER (WHERE is_system_skill), count(*) FILTER (WHERE NOT is_system_skill) FROM skill WHERE workspace_id = $1`, created.ID).Scan(&systems, &libraries)
			if systems != 10 || libraries != 5 {
				t.Fatalf("installed system/library skills = %d/%d, want 10/5", systems, libraries)
			}
		})
	}
}

func TestCreateWorkspace_ValidatesStartupTemplate(t *testing.T) {
	req := newRequest(http.MethodPost, "/api/workspaces", map[string]any{
		"name": "Invalid startup prompt", "slug": "handler-tests-invalid-startup", "context": "{{if .IssueID}}",
	})
	testutil.Call(t, testHandler.CreateWorkspace, req).Want(http.StatusBadRequest)
}

func TestUpdateWorkspace_ValidatesStartupTemplateBeforeWriting(t *testing.T) {
	const prompt = `{{.AgentName}} {{.AgentID}} {{.AgentInstructions}} {{.AgentSkills}}
{{.RequestingUserName}} {{.RequestingUserProfileDescription}}
{{.Repos}} {{.ProjectID}} {{.ProjectTitle}} {{.ProjectDescription}} {{.ProjectResources}}
{{.IssueStatuses}} {{.IssueStatusesOmitted}}
{{.OS}} {{.Provider}}`
	wsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name": "Startup prompt", "slug": "handler-tests-startup-prompt", "context": prompt,
	})
	req := withURLParam(newRequest(http.MethodPatch, "/api/workspaces/"+wsID, map[string]any{
		"name": "Rejected rename", "context": strings.ReplaceAll(prompt, "{{.AgentSkills}}", ""),
	}), "id", wsID)
	testutil.Call(t, testHandler.UpdateWorkspace, req).Want(http.StatusBadRequest)
	var name, context string
	dbfx.QueryRow(t, `SELECT name, context FROM workspace WHERE id = $1`, wsID).Scan(&name, &context)
	if name != "Startup prompt" || context != prompt {
		t.Fatalf("rejected edit changed workspace: name=%q, context=%q", name, context)
	}
	const edited = prompt + "\nEditable workspace instructions."
	req = withURLParam(newRequest(http.MethodPatch, "/api/workspaces/"+wsID, map[string]any{"context": edited}), "id", wsID)
	var updated WorkspaceResponse
	testutil.Call(t, testHandler.UpdateWorkspace, req).Want(http.StatusOK).JSON(&updated)
	if updated.Context == nil || *updated.Context != edited {
		t.Fatalf("saved startup prompt = %v, want %q", updated.Context, edited)
	}
}
