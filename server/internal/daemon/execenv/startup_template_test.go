package execenv

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInjectRuntimeConfigRendersWorkspaceTemplate(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		TaskID: "task-1", WorkspaceID: "workspace-1", IssueID: "issue-1",
		AgentInstructions: "Identity {{.NotATemplateField}}",
		WorkspaceContext:  "{{.Provider}}/{{.OS}} {{.WorkspaceID}}/{{.TaskID}}\n{{if .IssueID}}Issue {{.IssueID}}{{else}}Chat{{end}}\n{{.AgentInstructions}}\n{{range .Repos}}{{.URL}} {{.Ref}}\n{{end}}Workspace footer.",
		Repos:             []RepoContextForEnv{{URL: "https://github.com/example/repo", Ref: "feature"}},
	}
	want := "codex/" + runtime.GOOS + " workspace-1/task-1\nIssue issue-1\nIdentity {{.NotATemplateField}}\nhttps://github.com/example/repo feature\nWorkspace footer."
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	const original = "Repository rules.\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := InjectRuntimeConfig(dir, "codex", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("rendered prompt = %q, want %q", got, want)
	}
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(file), original) || !strings.Contains(string(file), want+"\n"+runtimeMarkerEnd) {
		t.Fatalf("runtime file did not preserve repository rules and rendered template: %s", file)
	}
	for _, source := range []string{"", "Plain workspace instructions."} {
		rendered, err := renderStartupPrompt("codex", TaskContextForEnv{WorkspaceContext: source})
		if err != nil || rendered != source {
			t.Fatalf("template %q: got %q, %v", source, rendered, err)
		}
	}
}

func TestInjectRuntimeConfigRejectsInvalidTemplateBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"{{if .IssueID}}", "{{.MissingField}}", "{{index .Repos 1}}"} {
		t.Run(source, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "AGENTS.md")
			const original = "Repository rules.\n"
			if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := InjectRuntimeConfig(dir, "codex", TaskContextForEnv{WorkspaceContext: source}); err == nil {
				t.Fatal("invalid template accepted")
			}
			file, err := os.ReadFile(path)
			if err != nil || string(file) != original {
				t.Fatalf("failed rendering changed repository instructions: %q, %v", file, err)
			}
		})
	}
}
