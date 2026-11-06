package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraecliPreparedSkillsKeepAllocatedPaths(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"prepare", "reuse"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			task := TaskContextForEnv{
				IssueID: "issue-traecli-skills",
				AgentSkills: []SkillContextForEnv{
					{
						Name: "Review", Description: "Review code.", Content: "Review body v1.",
						Files: []SkillFileContextForEnv{{Path: "references/checks.md", Content: "Checks v1."}},
					},
					{Name: "Review", Description: "Hidden description.", Content: "---\nname: hidden\ndisable-model-invocation: true\n---\n\nHidden body."},
					platformSkillFixture(),
				},
			}
			params := PrepareParams{
				WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-traecli-skills",
				TaskID: "task-traecli-skills", Provider: "traecli", Task: task,
			}
			workDir := t.TempDir()
			if phase == "reuse" {
				initial := params
				initial.Task.AgentSkills = nil
				env, err := Prepare(initial, testLogger())
				if err != nil {
					t.Fatal(err)
				}
				defer env.Cleanup(true)
				workDir = env.WorkDir
			}
			const userSkill = "User-owned skill."
			for _, name := range []string{"review", "multica-platform"} {
				mustWrite(t, filepath.Join(workDir, ".traecli", "skills", name, "SKILL.md"), userSkill)
			}
			var env *Environment
			if phase == "prepare" {
				params.LocalWorkDir = workDir
				var err error
				env, err = Prepare(params, testLogger())
				if err != nil {
					t.Fatal(err)
				}
				defer env.Cleanup(true)
			} else {
				reuse := ReuseParams{
					WorkspacesRoot: params.WorkspacesRoot, WorkDir: workDir, Provider: "traecli", Task: task,
				}
				if Reuse(reuse, testLogger()) == nil {
					t.Fatal("initial Reuse returned nil")
				}
				task.AgentSkills[0].Content = "Review body v2."
				task.AgentSkills[0].Files[0].Content = "Checks v2."
				reuse.Task = task
				env = Reuse(reuse, testLogger())
				if env == nil {
					t.Fatal("Reuse returned nil")
				}
			}
			for i, name := range []string{"review-multica", "review-multica-multica", "multica-platform-multica"} {
				relPath := ".traecli/skills/" + name + "/SKILL.md"
				data, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(relPath)))
				if err != nil {
					t.Fatal(err)
				}
				if i == 0 && !strings.Contains(string(data), task.AgentSkills[0].Content) {
					t.Errorf("skill body is stale: %s", data)
				}
			}
			ref, err := os.ReadFile(filepath.Join(workDir, ".traecli", "skills", "review-multica", "references", "checks.md"))
			if err != nil || string(ref) != task.AgentSkills[0].Files[0].Content {
				t.Errorf("supporting reference = %q, error = %v", ref, err)
			}
			for _, name := range []string{"review", "multica-platform"} {
				data, err := os.ReadFile(filepath.Join(workDir, ".traecli", "skills", name, "SKILL.md"))
				if err != nil || string(data) != userSkill {
					t.Errorf("user-owned skill %q changed: %q, error = %v", name, data, err)
				}
			}
		})
	}
}
