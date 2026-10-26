package execenv

import (
	"strings"
	"testing"
)

func TestRepositoriesKeepStartingPointsDiscoverable(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{"", "release/2026-09", "v1.4.0", "abc1234"} {
		t.Run(ref, func(t *testing.T) {
			out := buildMetaSkillContent("claude", TaskContextForEnv{
				IssueID: "i-1",
				Repos:   []RepoContextForEnv{{URL: "https://github.com/o/r", Description: "the platform", Ref: ref}},
			})
			for _, want := range []string{"https://github.com/o/r — the platform", "Configured starting points apply automatically", "Use `--ref <branch-or-sha>` only to override them"} {
				if !strings.Contains(out, want) {
					t.Errorf("missing repository context %q", want)
				}
			}
			if ref != "" && !strings.Contains(out, "(starts from `"+ref+"`)") {
				t.Errorf("missing starting point %q", ref)
			}
			if ref == "" && strings.Contains(out, "starts from") {
				t.Error("unpinned repository must not invent a starting point")
			}
		})
	}
	out := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "i-1"})
	if strings.Contains(out, "## Repositories") {
		t.Error("omit empty repository context")
	}
}

// The Project Context bullet used to read as though --ref were how you reach
// the configured revision. It is not: the daemon applies it, and --ref is the
// override. An agent following the old wording would re-specify a value it
// already had — and would keep using a stale one after the project changed.
func TestProjectContextSaysThePinIsAlreadyApplied(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1",
		ProjectID: "p-1", ProjectTitle: "Release line",
		ProjectResources: []ProjectResourceForEnv{{
			ResourceType: "github_repo",
			ResourceRef:  []byte(`{"url":"https://github.com/o/r","ref":"release/2026-09"}`),
		}},
	}
	out := buildMetaSkillContent("claude", ctx)

	if !strings.Contains(out, "checked out there automatically") {
		t.Errorf("Project Context should say the configured start is applied for you; got:\n%s", out)
	}
	if !strings.Contains(out, "only to override it") {
		t.Errorf("Project Context should frame --ref as the override; got:\n%s", out)
	}
}
