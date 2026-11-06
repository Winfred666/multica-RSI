package execenv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const platformSkillName = "multica-platform"

func platformSkillFixture() SkillContextForEnv {
	return SkillContextForEnv{Name: platformSkillName, BuiltinName: platformSkillName, Content: "---\nname: multica-platform\n---\n\nPlatform contracts."}
}

func TestPlatformSkillIdentityFollowsPreparedDirectories(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"claude", "traecli"} {
		for _, phase := range []string{"prepare", "reuse"} {
			for collisions := 0; collisions <= 2; collisions++ {
				t.Run(fmt.Sprintf("%s/%s/collisions-%d", provider, phase, collisions), func(t *testing.T) {
					t.Parallel()
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					task := TaskContextForEnv{IssueID: "issue-platform-identity", AgentSkills: []SkillContextForEnv{platformSkillFixture()}}
					params := PrepareParams{
						WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-platform-identity",
						TaskID: "task-platform-identity", Provider: provider, Task: task,
					}
					workDir := t.TempDir()
					if phase == "reuse" {
						initial := params
						initial.Task.AgentSkills = nil
						env, err := PrepareIsolated(ctx, preparationHelperTestCommand(), initial, testLogger())
						if err != nil {
							t.Fatal(err)
						}
						workDir = env.WorkDir
					}
					const userContent = "---\nname: multica-platform\n---\n\nUser-owned skill."
					for i := 0; i < collisions; i++ {
						mustWrite(t, filepath.Join(skillsDirPath(workDir, provider), skillSlugCandidate(platformSkillName, i), "SKILL.md"), userContent)
					}
					var env *Environment
					var err error
					if phase == "prepare" {
						params.LocalWorkDir = workDir
						env, err = PrepareIsolated(ctx, preparationHelperTestCommand(), params, testLogger())
					} else {
						// Refresh again after the first reuse materializes the platform
						// skill, so the assertions also cover reclaiming its old directory.
						for attempt := 0; attempt < 2; attempt++ {
							env, err = ReuseIsolated(ctx, preparationHelperTestCommand(), ReuseParams{
								WorkspacesRoot: params.WorkspacesRoot, WorkDir: workDir, Provider: provider, Task: task,
							}, testLogger())
							if err != nil || env == nil {
								t.Fatalf("reuse %d: environment=%+v, error=%v", attempt, env, err)
							}
						}
					}
					if err != nil || env == nil {
						t.Fatalf("%s: environment=%+v, error=%v", phase, env, err)
					}
					wantSlug := skillSlugCandidate(platformSkillName, collisions)
					if len(env.PreparedSkillNames) != 1 || env.PreparedSkillNames[0] != wantSlug {
						t.Fatalf("prepared names = %v, want [%s]", env.PreparedSkillNames, wantSlug)
					}
					prepared := ApplyPreparedSkillNames(task, env.PreparedSkillNames)
					visible := modelVisibleSkills(prepared.AgentSkills)
					if len(visible) != 1 || visible[0].BuiltinName != platformSkillName || task.AgentSkills[0].Name != platformSkillName {
						t.Fatalf("builtin identity lost or original mutated: prepared=%+v, original=%+v", visible, task.AgentSkills)
					}
					data, err := os.ReadFile(filepath.Join(skillsDirPath(workDir, provider), wantSlug, "SKILL.md"))
					if err != nil || !frontmatterNameIs(string(data), wantSlug) {
						t.Fatalf("allocated skill = %q, error=%v", data, err)
					}
					for i := 0; i < collisions; i++ {
						data, err := os.ReadFile(filepath.Join(skillsDirPath(workDir, provider), skillSlugCandidate(platformSkillName, i), "SKILL.md"))
						if err != nil || string(data) != userContent {
							t.Fatalf("user skill changed: %q, error=%v", data, err)
						}
					}
				})
			}
		}
	}
}
