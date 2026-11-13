package execenv

import (
	"strings"
	"testing"
)

func TestBuildCommentReplyInstructionsNaturalCompletion(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	for _, host := range []string{"linux", "darwin", "windows"} {
		runtimeGOOS = host
		for _, provider := range []string{"codex", "claude", "hermes", "pi", "opencode", "openclaw", "kimi", "reasonix", "kiro", "cursor"} {
			for _, leader := range []bool{false, true} {
				got := BuildCommentReplyInstructions(provider, "issue-id", "current-trigger", leader)
				for _, want := range []string{"Answer naturally", "Successful completion publishes", "issue-id under trigger comment current-trigger", "quiet/no_action", "leave the final response empty"} {
					if !strings.Contains(got, want) {
						t.Fatalf("%s/%s leader=%v: missing %q: %s", host, provider, leader, want, got)
					}
				}
				for _, banned := range []string{"reply.md", "--content-file", "multica issue comment add", "Remove-Item", "rm ./"} {
					if strings.Contains(got, banned) {
						t.Fatalf("%s/%s: forced posting %q: %s", host, provider, banned, got)
					}
				}
			}
		}
	}
	if got := BuildCommentReplyInstructions("pi", "issue-id", "", false); got != "" {
		t.Fatalf("no trigger: %q", got)
	}
}
