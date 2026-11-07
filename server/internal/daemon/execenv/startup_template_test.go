package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPackagedStartupNeedsNoWorkflowSkill(t *testing.T) {
	source, err := os.ReadFile("../../../../harness/startup.gotmpl")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateStartupTemplate(string(source)); err != nil {
		t.Fatal(err)
	}
	for _, ctx := range []TaskContextForEnv{
		{IssueID: "issue-1"}, {IssueID: "issue-1", HasProjectWorkflow: true},
		{ChatSessionID: "chat-1"}, {QuickCreatePrompt: "create an issue"}, {AutopilotRunID: "run-1"},
	} {
		ctx.WorkspaceContext = string(source)
		out, err := renderStartupPrompt("codex", ctx)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "Use `multica-issue-turn`") || !strings.Contains(out, "Never-HERO (Hashing, Edge cases, Rubrics, Overbuild)") || !strings.Contains(out, `"Stop-slop": Replies and deliverables`) {
			t.Fatalf("startup still requires workflow retrieval or lacks scope guidance: %s", out)
		}
		if ctx.IssueID != "" {
			for _, rule := range []string{"--roots-only --summary --compact", "[Project workflow]", "done` stays human", "--content-file", "Agent mentions start runs", "multica-subissue-review"} {
				if !strings.Contains(out, rule) {
					t.Errorf("startup lost essential contract %q", rule)
				}
			}
		}
		if !strings.HasSuffix(strings.TrimSpace(out), "State uncertainty, blocked and failed checks once only if impact goal-reaching directly.") {
			t.Fatal("scope and writing guidance must be the final prompt block")
		}
	}
	raw, err := os.ReadFile("../../../../harness/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SystemSkills []string `json:"system_skills"`
		Skills       []string `json:"skills"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, name := range append(manifest.SystemSkills, manifest.Skills...) {
		if name == "multica-issue-turn" {
			t.Fatal("retired workflow skill still installs")
		}
	}
}

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
