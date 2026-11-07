package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/harness"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func workspaceHarnessRequest(body any) *http.Request {
	r := newRequest(http.MethodPost, "/api/workspaces", body)
	return r
}

func cleanupWorkspaceHarness(t *testing.T, workspaceID string) {
	t.Helper()
	dbfx.Cleanup(t, `DELETE FROM skill WHERE workspace_id = $1`, workspaceID)
	dbfx.Cleanup(t, `DELETE FROM skill_file WHERE skill_id IN (SELECT id FROM skill WHERE workspace_id = $1)`, workspaceID)
}

func TestCreateWorkspace_InstallsStartupKit(t *testing.T) {
	// Installation must not resolve Python/CLI executables or call the public URL.
	t.Setenv("PATH", t.TempDir())
	previous := testHandler.cfg.PublicURL
	testHandler.cfg.PublicURL = "http://127.0.0.1:1"
	t.Cleanup(func() { testHandler.cfg.PublicURL = previous })
	manifestRaw, err := harness.Files.ReadFile("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SystemSkills []string `json:"system_skills"`
		Skills       []string `json:"skills"`
	}
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
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
			if systems != len(manifest.SystemSkills) || libraries != len(manifest.Skills) {
				t.Fatalf("installed system/library skills = %d/%d, want %d/%d", systems, libraries, len(manifest.SystemSkills), len(manifest.Skills))
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
