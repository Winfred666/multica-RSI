package execenv

import (
	"strings"
	"testing"
)

// TestBuildCommentReplyInstructionsCodexLinux pins that the Linux/macOS
// reply template now mandates `--content-file` (post-#4182). The previous
// `--content-stdin` + HEREDOC mandate (#1795 / #1851 / MUL-2904) was kept
// for years to defend against backtick / `$()` substitution in the body,
// but the heredoc/flag boundary turned out to be fragile in its own right:
// when a model wrapped extra flags around the heredoc on `multica issue
// create`, the flags got swallowed into stdin and silently dropped (OXY-78,
// OXY-76). The file path defeats both classes — the body never reaches the
// shell, and all flags live on one shell-token line.
//
// Not parallel: mutates the package-level runtimeGOOS.
func TestBuildCommentReplyInstructionsCodexLinux(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "linux"

	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	got := BuildCommentReplyInstructions("codex", issueID, triggerID, false)

	for _, want := range []string{
		"multica issue comment add " + issueID + " --parent " + triggerID + " --content-file ./reply.md --output table && rm ./reply.md",
		"Write the body file first",
		"--content-file ./reply.md",
		"#4182",
		"Keep the `&&`",
		"Do NOT write literal `\\n` escapes to simulate line breaks",
		"do NOT reuse --parent values from previous turns",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("codex/linux reply instructions missing %q\n---\n%s", want, got)
		}
	}

	for _, banned := range []string{
		"--content \"...\"",
		"<<'COMMENT'",
		"cat <<",
		"--parent " + triggerID + " --content-stdin",
	} {
		if strings.Contains(got, banned) {
			t.Fatalf("codex/linux reply instructions should not contain %q\n---\n%s", banned, got)
		}
	}
}

// TestBuildCommentReplyInstructionsNonCodexLinux pins that EVERY provider on
// Linux/macOS — not just Codex — gets the `--content-file` template. Two
// shell-driven failure classes motivate the uniform file path:
//   - MUL-2904 / OKK-497: an agent inlined a backtick-wrapped table name into
//     `--content`; the shell ran it as a command substitution, silently deleted
//     it, the stored comment no longer matched the model's intent, and the
//     model retried forever.
//   - GitHub #4182 (OXY-78 / OXY-76): an agent wrapped extra flags around an
//     `--content-stdin` HEREDOC; the bash heredoc/flag boundary swallowed
//     `--assignee` / `--project` into stdin or dropped them as failed
//     standalone shell statements, while the create still exited 0 with nulls.
//
// Both classes are shell-driven, so the guardrail is uniform across providers
// and across hosts.
//
// Not parallel: mutates the package-level runtimeGOOS.
func TestBuildCommentReplyInstructionsNonCodexLinux(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })

	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	for _, host := range []string{"linux", "darwin"} {
		for _, provider := range []string{"claude", "opencode", "openclaw", "hermes", "kimi", "reasonix", "kiro", "cursor"} {
			name := provider + "/" + host
			t.Run(name, func(t *testing.T) {
				runtimeGOOS = host
				got := BuildCommentReplyInstructions(provider, issueID, triggerID, false)

				for _, want := range []string{
					"multica issue comment add " + issueID + " --parent " + triggerID + " --content-file ./reply.md --output table && rm ./reply.md",
					// MUL-5442 cross-channel dedup: shell-hazard mechanics live in
					// the brief's Comment Formatting; the cookbook keeps the
					// file-first order, the command, and the pointer.
					"Write the body file first",
					"## Comment Formatting",
					"#4182",
					"Keep the `&&`",
					"do NOT reuse --parent values from previous turns",
					"Post your reply as a comment",
				} {
					if !strings.Contains(got, want) {
						t.Errorf("%s reply instructions missing %q\n---\n%s", name, want, got)
					}
				}

				// The two regressions: agent-authored comments must never be
				// steered at inline `--content "..."` (MUL-2904) and never at
				// `--content-stdin` HEREDOC on multi-flag commands (#4182).
				for _, banned := range []string{
					"--content \"...\"",
					"<<'COMMENT'",
					"cat <<",
					"--parent " + triggerID + " --content-stdin",
				} {
					if strings.Contains(got, banned) {
						t.Errorf("%s reply instructions still contains %q\n---\n%s", name, banned, got)
					}
				}
			})
		}
	}
}

// TestBuildCommentReplyInstructionsWindowsUsesContentFile pins that on
// Windows every provider — Codex AND non-Codex — gets the
// `--content-file` template. The bug is shell-layer, not provider-layer:
// any agent on Windows piping HEREDOC through PowerShell loses non-ASCII
// bytes (PS 5.1's `$OutputEncoding` defaults to ASCIIEncoding). Issues
// #2198 (Chinese, Codex), #2236 (Chinese, Codex), #2376 (Cyrillic,
// non-Codex agent name) all match this signature.
//
// Not parallel: mutates the package-level runtimeGOOS.
func TestBuildCommentReplyInstructionsWindowsUsesContentFile(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "windows"

	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	for _, provider := range []string{"codex", "claude", "opencode", "openclaw", "hermes", "kimi", "reasonix", "kiro", "cursor"} {
		t.Run(provider+"/windows", func(t *testing.T) {
			got := BuildCommentReplyInstructions(provider, issueID, triggerID, false)
			for _, want := range []string{
				"multica issue comment add " + issueID + " --parent " + triggerID + " --content-file ./reply.md --output table",
				// MUL-5442 cross-channel dedup: the $OutputEncoding trap's
				// full mechanics live once, in the brief's Windows Comment
				// Formatting variant; the per-turn cookbook keeps the ban,
				// the one-line consequence, and the pointer.
				"if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }",
				// #8627: the host OS does not fix the agent's shell — Claude
				// Code with CLAUDE_CODE_USE_POWERSHELL_TOOL=0 runs Git Bash —
				// so the Windows cookbook carries a `&&` variant too.
				"Git Bash",
				"multica issue comment add " + issueID + " --parent " + triggerID + " --content-file ./reply.md --output table && rm ./reply.md",
				"Write the body file first",
				"never pipe via `--content-stdin`",
				"PowerShell drops non-ASCII",
				"## Comment Formatting",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("%s reply instructions missing %q\n---\n%s", provider, want, got)
				}
			}
			for _, banned := range []string{
				"<<'COMMENT'",
				"--parent " + triggerID + " --content-stdin",
				"cat <<",
			} {
				if strings.Contains(got, banned) {
					t.Errorf("%s/windows reply instructions should not contain %q\n---\n%s", provider, banned, got)
				}
			}
		})
	}
}

func TestBuildCommentReplyInstructionsEmptyWhenNoTrigger(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{"codex", "claude", "opencode"} {
		if got := BuildCommentReplyInstructions(provider, "issue-id", "", false); got != "" {
			t.Fatalf("expected empty string when triggerCommentID is empty for %s, got %q", provider, got)
		}
	}
}

// Windows reply instructions are file-first, never stdin. The instructions
// now ship in the per-turn prompt, so pin the helper directly (MUL-5377).
func TestWindowsCommentReplyInstructionsHaveNoStdin(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "windows"

	issueID := "11111111-1111-1111-1111-111111111111"
	triggerID := "22222222-2222-2222-2222-222222222222"

	for _, provider := range []string{"claude", "codex", "opencode"} {
		t.Run(provider, func(t *testing.T) {
			s := BuildCommentReplyInstructions(provider, issueID, triggerID, false)
			for _, want := range []string{
				"multica issue comment add " + issueID + " --parent " + triggerID + " --content-file",
				"--content-file",
			} {
				if !strings.Contains(s, want) {
					t.Errorf("%s reply instructions missing %q\n---\n%s", provider, want, s)
				}
			}
			for _, banned := range []string{
				"--parent " + triggerID + " --content-stdin",
				"always use `--content-stdin` with a HEREDOC, even for short single-line replies",
			} {
				if strings.Contains(s, banned) {
					t.Errorf("%s reply instructions must not prescribe stdin on Windows: %q\n---\n%s", provider, banned, s)
				}
			}
		})
	}
}

// TestBuildCommentReplyInstructionsSquadLeaderCarveOut pins that the squad
// leader variant scopes the reply imperative with the `no_action` exception
// (MUL-5442 #6493 review): the leader's only silent path must not be
// contradicted by a later unconditional "Post your reply" line, and the
// ordinary variant must never carry the leader carve-out.
func TestBuildCommentReplyInstructionsSquadLeaderCarveOut(t *testing.T) {
	t.Parallel()

	leader := BuildCommentReplyInstructions("claude", "issue-1", "comment-1", true)
	if !strings.Contains(leader, "Unless your outcome is `no_action`, post your reply as a comment") {
		t.Errorf("leader reply instructions missing the no_action carve-out\n---\n%s", leader)
	}
	if strings.Contains(leader, "Post your reply as a comment") {
		t.Errorf("leader reply instructions still carry the unconditional imperative\n---\n%s", leader)
	}

	ordinary := BuildCommentReplyInstructions("claude", "issue-1", "comment-1", false)
	if !strings.Contains(ordinary, "Post your reply as a comment") {
		t.Errorf("ordinary reply instructions missing the imperative\n---\n%s", ordinary)
	}
	if strings.Contains(ordinary, "Unless your outcome is") {
		t.Errorf("ordinary reply instructions leaked the leader carve-out\n---\n%s", ordinary)
	}
}
