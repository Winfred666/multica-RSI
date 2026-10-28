package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

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
