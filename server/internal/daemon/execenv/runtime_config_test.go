package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommentHintsCarryNoModality pins MUL-6984: the per-turn comment hints
// carry this turn's facts and exact commands and never decide whether the
// wide read happens — workflow step 2 owns that. Each hint hands the scan over
// (or, on the resumed no-delta path, reports the server-computed answer to
// it); none of them makes it conditional on the agent's own guess.
func TestCommentHintsCarryNoModality(t *testing.T) {
	t.Parallel()
	const issueID = "55555555-6666-7777-8888-999999999999"
	hints := map[string]string{
		"cold":    BuildColdCommentsHint(issueID, "trigger-1", "thread-root-1"),
		"warm":    BuildNewCommentsHint(issueID, "trigger-1", "thread-root-1", "2026-05-28T11:00:00Z", 4),
		"resumed": BuildResumedCommentsHint(issueID, "trigger-1", "thread-root-1"),
	}
	for name, hint := range hints {
		if hint == "" {
			t.Fatalf("%s hint rendered empty", name)
		}
		for _, banned := range []string{
			"Only if you need",
			"Need cross-thread background",
			"If your reply depends on thread context",
			"read them all blindly",
			"only if needed",
		} {
			if strings.Contains(hint, banned) {
				t.Errorf("%s hint must not make the wide read optional (%q):\n%s", name, banned, hint)
			}
		}
	}
	// Cold has no server-computed delta, so it hands over the scan itself. Warm
	// has one, so it hands over the read that IS the scan's answer — a single
	// issue-wide `--since` (MUL-7344). Both are unconditional commands.
	if !strings.Contains(hints["cold"], "--roots-only --summary") {
		t.Errorf("cold hint must hand over the scan step 2 requires:\n%s", hints["cold"])
	}
	if !strings.Contains(hints["warm"], "--since 2026-05-28T11:00:00Z --compact --output json") {
		t.Errorf("warm hint must hand over the issue-wide delta read:\n%s", hints["warm"])
	}
	if strings.Contains(hints["warm"], "--roots-only --summary") {
		t.Errorf("warm hint must not also hand over the scan the delta read answers:\n%s", hints["warm"])
	}
	if !strings.Contains(hints["resumed"], "issue-wide delta is empty") {
		t.Errorf("resumed hint must report the empty delta as the scan's answer:\n%s", hints["resumed"])
	}
}

// Cold-start thread routing moved to the per-turn prompt (MUL-5377); the
// helper behaviour it relies on is pinned here directly.
func TestColdCommentsHintPointsAtTriggeringThread(t *testing.T) {
	t.Parallel()
	const issueID = "55555555-6666-7777-8888-999999999999"
	hint := BuildColdCommentsHint(issueID, "trigger-1", "thread-root-1")
	if strings.Contains(hint, "new comment(s) since your last run") {
		t.Errorf("no since-delta hint should render on cold start, got:\n%s", hint)
	}
	if !strings.Contains(hint, "multica issue comment list "+issueID+" --thread thread-root-1 --tail 30 --compact --output json") {
		t.Errorf("cold start must point at the triggering thread read, got:\n%s", hint)
	}

}

// Resumed/no-delta routing moved to the per-turn prompt (MUL-5377).
func TestResumedCommentsHintSkipsDefaultThreadRead(t *testing.T) {
	t.Parallel()
	const issueID = "55555555-6666-7777-8888-999999999999"
	hint := BuildResumedCommentsHint(issueID, "trigger-1", "thread-root-1")

	for _, want := range []string{
		"triggering comment is already included above",
		"No other new comments on this issue since your last run",
		"issue-wide delta is empty",
		"if resumed memory is not enough",
		"multica issue comment list " + issueID + " --thread thread-root-1 --tail 30 --compact --output json",
	} {
		if !strings.Contains(hint, want) {
			t.Errorf("resumed/no-delta hint missing %q\n--- output ---\n%s", want, hint)
		}
	}
	// The anchor-restating sentence is gone (MUL-5721 OPT-1): the read command
	// carries the thread anchor and the reply cookbook carries the trigger id.
	if strings.Contains(hint, "active thread anchor") {
		t.Errorf("resumed/no-delta hint must not restate anchors outside the commands, got:\n%s", hint)
	}
	if strings.Contains(hint, "scoped to the triggering thread") {
		t.Errorf("resumed/no-delta hint must not claim the delta is thread-scoped, got:\n%s", hint)
	}
	if strings.Contains(hint, "in place of `--thread ... --tail 30`") {
		t.Errorf("resumed/no-delta hint must not render the reconstruction (cold) hint, got:\n%s", hint)
	}
}

func TestConnectedAppsHeadingSkippedWhenEmpty(t *testing.T) {
	t.Parallel()
	out := BuildConnectedAppsBlock(nil)
	if strings.Contains(out, "## Connected Apps") {
		t.Fatalf("empty connected apps must not emit the heading")
	}
}

// writeRuntimeConfigFile is the safe replacement for the previous
// unconditional os.WriteFile of CLAUDE.md / AGENTS.md. The two
// states it must handle correctly are: file missing, file present without
// markers (user-authored content already there — the regression case from
// MUL-2753), and file present with markers (idempotent second-run replace).

func TestWriteRuntimeConfigFileCreatesMissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const brief = "# Multica Agent Runtime\n\nbrief body line"

	if err := writeRuntimeConfigFile(path, brief); err != nil {
		t.Fatalf("writeRuntimeConfigFile returned error: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	s := string(got)
	if !strings.HasPrefix(s, runtimeMarkerBegin+"\n") {
		t.Errorf("output should start with begin marker, got:\n%s", s)
	}
	if !strings.Contains(s, brief) {
		t.Errorf("output should contain brief body, got:\n%s", s)
	}
	if !strings.Contains(s, "\n"+runtimeMarkerEnd+"\n") {
		t.Errorf("output should contain end marker followed by newline, got:\n%s", s)
	}
}

func TestWriteRuntimeConfigFilePreservesUserContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const userContent = "# User repo CLAUDE.md\n\n- rule one\n- rule two\n"
	if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
		t.Fatalf("seed user file: %v", err)
	}

	const brief = "## Multica brief\n\ninjected body"
	if err := writeRuntimeConfigFile(path, brief); err != nil {
		t.Fatalf("writeRuntimeConfigFile returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	s := string(got)
	// The user's original content must be untouched and appear before the
	// injected marker block; this is the core regression case from MUL-2753.
	if !strings.HasPrefix(s, userContent) {
		t.Errorf("user content must be preserved verbatim at the top of the file, got:\n%s", s)
	}
	beginIdx := strings.Index(s, runtimeMarkerBegin)
	endIdx := strings.Index(s, runtimeMarkerEnd)
	if beginIdx < 0 || endIdx <= beginIdx {
		t.Fatalf("expected a well-formed marker block in:\n%s", s)
	}
	if beginIdx < len(userContent) {
		t.Errorf("begin marker must appear after user content, beginIdx=%d userLen=%d", beginIdx, len(userContent))
	}
	if !strings.Contains(s, brief) {
		t.Errorf("brief body missing from output:\n%s", s)
	}
}

func TestWriteRuntimeConfigFileReplacesExistingBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	const userBefore = "# User AGENTS.md\n\nuser line above\n"
	const userAfter = "\nuser line below the block\n"
	original := userBefore +
		runtimeMarkerBegin + "\n" +
		"OLD BRIEF CONTENT THAT MUST GO AWAY\n" +
		runtimeMarkerEnd + "\n" +
		userAfter
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const newBrief = "## New Multica brief\n\nfresh body"
	if err := writeRuntimeConfigFile(path, newBrief); err != nil {
		t.Fatalf("writeRuntimeConfigFile returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	s := string(got)
	if !strings.HasPrefix(s, userBefore) {
		t.Errorf("content above the marker block must be preserved, got:\n%s", s)
	}
	if !strings.HasSuffix(s, userAfter) {
		t.Errorf("content below the marker block must be preserved, got:\n%s", s)
	}
	if strings.Contains(s, "OLD BRIEF CONTENT THAT MUST GO AWAY") {
		t.Errorf("previous block body must be replaced, got:\n%s", s)
	}
	if !strings.Contains(s, newBrief) {
		t.Errorf("new brief body missing from output:\n%s", s)
	}
	if strings.Count(s, runtimeMarkerBegin) != 1 || strings.Count(s, runtimeMarkerEnd) != 1 {
		t.Errorf("there must be exactly one begin/end marker pair, got:\n%s", s)
	}
}

func TestWriteRuntimeConfigFileIsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const userContent = "# User CLAUDE.md\n\nimportant rules\n"
	if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
		t.Fatalf("seed user file: %v", err)
	}

	const brief = "## Multica brief\n\nbody"
	for i := 0; i < 5; i++ {
		if err := writeRuntimeConfigFile(path, brief); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	s := string(got)
	if strings.Count(s, runtimeMarkerBegin) != 1 {
		t.Errorf("repeated runs must not duplicate the begin marker, count=%d, file:\n%s", strings.Count(s, runtimeMarkerBegin), s)
	}
	if strings.Count(s, runtimeMarkerEnd) != 1 {
		t.Errorf("repeated runs must not duplicate the end marker, count=%d, file:\n%s", strings.Count(s, runtimeMarkerEnd), s)
	}
	if strings.Count(s, brief) != 1 {
		t.Errorf("repeated runs must not duplicate the brief body, count=%d, file:\n%s", strings.Count(s, brief), s)
	}
	if !strings.HasPrefix(s, userContent) {
		t.Errorf("user content must remain intact at the top of the file, got:\n%s", s)
	}
}

// InjectRuntimeConfig is the production entry point — verify the marker
// semantics propagate through it for each provider's target filename.
func TestInjectRuntimeConfigPreservesUserContent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider string
		filename string
	}{
		{"claude", "CLAUDE.md"},
		{"codebuddy", "CODEBUDDY.md"},
		{"codex", "AGENTS.md"},
		{"copilot", "AGENTS.md"},
		{"opencode", "AGENTS.md"},
		{"codearts", "AGENTS.md"},
		{"openclaw", "AGENTS.md"},
		{"hermes", "AGENTS.md"},
		{"pi", "AGENTS.md"},
		{"omp", "AGENTS.md"},
		{"cursor", "AGENTS.md"},
		{"kimi", "AGENTS.md"},
		{"reasonix", "AGENTS.md"},
		{"dsh", "AGENTS.md"},
		{"dim", "AGENTS.md"},
		{"zeroclaw", "AGENTS.md"},
		{"kiro", "AGENTS.md"},
		{"antigravity", "AGENTS.md"},
		{"muse", "AGENTS.md"},
		{"qwen", "QWEN.md"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, tc.filename)
			const userContent = "# User-authored file\n\ndon't touch this\n"
			if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			content, err := InjectRuntimeConfig(dir, tc.provider, TaskContextForEnv{
				IssueID:          "11111111-2222-3333-4444-555555555555",
				WorkspaceContext: "Issue {{.IssueID}}",
			})
			if err != nil {
				t.Fatalf("InjectRuntimeConfig: %v", err)
			}
			if content == "" {
				t.Fatalf("returned brief content must be non-empty")
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			s := string(got)
			if !strings.HasPrefix(s, userContent) {
				t.Errorf("[%s] user content must be preserved verbatim at the top of %s, got:\n%s", tc.provider, tc.filename, s)
			}
			if !strings.Contains(s, runtimeMarkerBegin) || !strings.Contains(s, runtimeMarkerEnd) {
				t.Errorf("[%s] %s must contain the runtime marker block, got:\n%s", tc.provider, tc.filename, s)
			}
		})
	}
}

// CodeBuddy is a Claude Code fork but ships its own native config
// directory (~/.codebuddy, .codebuddy/) rather than reusing Claude's
// ~/.claude / CLAUDE.md paths (see
// https://www.codebuddy.ai/docs/cli/codebuddy-dir). This pins the two
// providers to different target filenames so a future edit can't
// silently re-merge them.
func TestRuntimeConfigPathDistinguishesCodebuddyFromClaude(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	claudePath := runtimeConfigPath(dir, "claude")
	codebuddyPath := runtimeConfigPath(dir, "codebuddy")

	if claudePath != filepath.Join(dir, "CLAUDE.md") {
		t.Errorf("claude runtime config path = %q, want CLAUDE.md", claudePath)
	}
	if codebuddyPath != filepath.Join(dir, "CODEBUDDY.md") {
		t.Errorf("codebuddy runtime config path = %q, want CODEBUDDY.md", codebuddyPath)
	}
	if claudePath == codebuddyPath {
		t.Fatal("claude and codebuddy must not share a runtime config path")
	}
}

func TestInjectRuntimeConfigUnknownProviderSkipsWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Seed all candidate filenames so we can verify none of them get
	// written when the provider is unknown.
	for _, name := range []string{"CLAUDE.md", "CODEBUDDY.md", "AGENTS.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("untouched\n"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	if _, err := InjectRuntimeConfig(dir, "totally-unknown-provider", TaskContextForEnv{
		IssueID:          "11111111-2222-3333-4444-555555555555",
		WorkspaceContext: "Issue {{.IssueID}}",
	}); err != nil {
		t.Fatalf("InjectRuntimeConfig: %v", err)
	}
	for _, name := range []string{"CLAUDE.md", "CODEBUDDY.md", "AGENTS.md"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != "untouched\n" {
			t.Errorf("unknown provider must not write %s; got:\n%s", name, string(got))
		}
	}
}

// Parser hardening: the end marker must be found strictly after the begin
// marker so a stray end marker that appears earlier in user content (e.g.
// a documentation snippet showing what the wire format looks like) doesn't
// trick writeRuntimeConfigFile into thinking the file is malformed and
// appending another block on every run.
func TestWriteRuntimeConfigFileIgnoresStrayEndMarkerBeforeBegin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// Seed a file whose user-authored portion documents the marker format
	// (so the *end* marker appears before any *begin* marker), then has a
	// real block authored by an earlier Multica run below.
	const userDoc = "# Repo CLAUDE.md\n\nExample of what Multica writes:\n" +
		runtimeMarkerEnd + "\n\n# Real config below\n"
	original := userDoc +
		runtimeMarkerBegin + "\nFIRST BRIEF\n" + runtimeMarkerEnd + "\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const newBrief = "SECOND BRIEF"
	if err := writeRuntimeConfigFile(path, newBrief); err != nil {
		t.Fatalf("writeRuntimeConfigFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)

	// The user's stray end marker line plus surrounding doc text must still
	// be present, and the file must contain exactly one begin marker and
	// one *additional* end marker (so two end markers total — the stray
	// one and the one closing our block).
	if !strings.Contains(s, userDoc) {
		t.Errorf("user doc with stray end marker must be preserved verbatim, got:\n%s", s)
	}
	if got, want := strings.Count(s, runtimeMarkerBegin), 1; got != want {
		t.Errorf("expected exactly %d begin markers, got %d:\n%s", want, got, s)
	}
	if got, want := strings.Count(s, runtimeMarkerEnd), 2; got != want {
		t.Errorf("expected exactly %d end markers (1 user stray + 1 closing our block), got %d:\n%s", want, got, s)
	}
	if strings.Contains(s, "FIRST BRIEF") {
		t.Errorf("previous brief body must be replaced, got:\n%s", s)
	}
	if !strings.Contains(s, newBrief) {
		t.Errorf("new brief body missing from output:\n%s", s)
	}

	// Idempotency under the stray-end pattern: a second write must not
	// stack another block.
	if err := writeRuntimeConfigFile(path, newBrief); err != nil {
		t.Fatalf("second writeRuntimeConfigFile: %v", err)
	}
	got2, _ := os.ReadFile(path)
	s2 := string(got2)
	if got, want := strings.Count(s2, runtimeMarkerBegin), 1; got != want {
		t.Errorf("repeat write must not grow begin markers, got %d, want %d:\n%s", got, want, s2)
	}
}

// Parser hardening: a file containing only a begin marker (e.g. a previous
// run that crashed mid-write) must not cause every subsequent run to stack
// another block beneath the half-block.
func TestWriteRuntimeConfigFileReplacesMalformedHalfBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	const userTop = "# Repo AGENTS.md\n\nrules above\n"
	const halfBlock = "leftover from crashed write\nsecond line\n"
	original := userTop + runtimeMarkerBegin + "\n" + halfBlock
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const newBrief = "recovered brief"
	if err := writeRuntimeConfigFile(path, newBrief); err != nil {
		t.Fatalf("writeRuntimeConfigFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)
	if !strings.HasPrefix(s, userTop) {
		t.Errorf("user content above the half-block must be preserved, got:\n%s", s)
	}
	if strings.Contains(s, "leftover from crashed write") {
		t.Errorf("half-block contents must be replaced, got:\n%s", s)
	}
	if got, want := strings.Count(s, runtimeMarkerBegin), 1; got != want {
		t.Errorf("expected exactly %d begin marker, got %d:\n%s", want, got, s)
	}
	if got, want := strings.Count(s, runtimeMarkerEnd), 1; got != want {
		t.Errorf("expected exactly %d end marker after recovery, got %d:\n%s", want, got, s)
	}
	if !strings.Contains(s, newBrief) {
		t.Errorf("new brief body missing from output:\n%s", s)
	}
}

// Cleanup excises the marker block, preserving every byte of surrounding
// user content. This is the local_directory invariant: a `claude` /
// `codex` run started by the user after a Multica task must see the same
// file the user wrote.
func TestCleanupRuntimeConfigPreservesUserContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	const userBefore = "# Repo CLAUDE.md\n\nuser line above\n"
	const userAfter = "\nuser line below the block\n"
	const userExpected = "# Repo CLAUDE.md\n\nuser line above\n\nuser line below the block\n"
	// Inject via the production write path so we exercise the actual
	// marker block format, not a hand-rolled approximation.
	if err := os.WriteFile(path, []byte(userBefore+userAfter), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := writeRuntimeConfigFile(path, "brief body"); err != nil {
		t.Fatalf("seed brief: %v", err)
	}

	if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
		t.Fatalf("CleanupRuntimeConfig: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)
	if strings.Contains(s, runtimeMarkerBegin) || strings.Contains(s, runtimeMarkerEnd) {
		t.Errorf("marker block must be removed, got:\n%s", s)
	}
	if strings.Contains(s, "brief body") {
		t.Errorf("brief body must be removed, got:\n%s", s)
	}
	if s != userExpected {
		t.Errorf("user content must be preserved byte-for-byte\n got:\n%q\nwant:\n%q", s, userExpected)
	}
}

// Cleanup removes the file entirely when the marker block was the only
// content — i.e. we created the file from scratch in a directory that had
// no pre-existing CLAUDE.md / AGENTS.md.
func TestCleanupRuntimeConfigRemovesFileWhenOnlyBlockRemained(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// No seed — writeRuntimeConfigFile creates the file with only the
	// marker block inside.
	if err := writeRuntimeConfigFile(path, "brief body"); err != nil {
		t.Fatalf("seed brief: %v", err)
	}

	if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
		t.Fatalf("CleanupRuntimeConfig: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to be removed, stat err=%v", err)
	}
}

// Cleanup is a no-op when no marker block exists or when the file is
// missing — Cleanup is safe to call defensively from the daemon's defer.
func TestCleanupRuntimeConfigNoOpCases(t *testing.T) {
	t.Parallel()

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
			t.Errorf("missing file must be no-op, got: %v", err)
		}
		// And the directory must remain untouched.
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("readdir: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected dir to remain empty, got: %v", entries)
		}
	})

	t.Run("file without marker block", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "CLAUDE.md")
		const userContent = "# Repo CLAUDE.md\n\nrules\n"
		if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
			t.Errorf("no-marker-block file must be no-op, got: %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if string(got) != userContent {
			t.Errorf("file must be untouched\n got:\n%q\nwant:\n%q", string(got), userContent)
		}
	})

	t.Run("unknown provider", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		// Seed every candidate filename to verify none of them get touched.
		for _, name := range []string{"CLAUDE.md", "CODEBUDDY.md", "AGENTS.md"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("untouched\n"), 0o644); err != nil {
				t.Fatalf("seed %s: %v", name, err)
			}
		}
		if err := CleanupRuntimeConfig(dir, "totally-unknown-provider"); err != nil {
			t.Errorf("unknown provider must be no-op, got: %v", err)
		}
		for _, name := range []string{"CLAUDE.md", "CODEBUDDY.md", "AGENTS.md"} {
			got, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			if string(got) != "untouched\n" {
				t.Errorf("unknown provider must not touch %s; got:\n%s", name, string(got))
			}
		}
	})
}

// Cleanup must handle a half-block left by a previous crashed run: begin
// marker present but no end. Otherwise the half-block would survive
// cleanup and pollute the next manual CLI invocation in the same dir.
func TestCleanupRuntimeConfigRemovesMalformedHalfBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	const userTop = "# Repo AGENTS.md\n\nrules\n"
	original := userTop + runtimeMarkerBegin + "\nhalf-written brief no end\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := CleanupRuntimeConfig(dir, "codex"); err != nil {
		t.Fatalf("CleanupRuntimeConfig: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)
	if strings.Contains(s, runtimeMarkerBegin) {
		t.Errorf("half-block begin marker must be excised, got:\n%s", s)
	}
	if strings.Contains(s, "half-written brief no end") {
		t.Errorf("half-block body must be excised, got:\n%s", s)
	}
	if !strings.HasPrefix(s, userTop) {
		t.Errorf("user content above the half-block must remain, got:\n%s", s)
	}
}

// Cleanup must remove the marker block for every provider's target file,
// using the same provider→filename mapping as InjectRuntimeConfig — so a
// new provider added to one side cannot drift past the other.
func TestCleanupRuntimeConfigByProvider(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider string
		filename string
	}{
		{"claude", "CLAUDE.md"},
		{"codebuddy", "CODEBUDDY.md"},
		{"codex", "AGENTS.md"},
		{"copilot", "AGENTS.md"},
		{"opencode", "AGENTS.md"},
		{"codearts", "AGENTS.md"},
		{"openclaw", "AGENTS.md"},
		{"hermes", "AGENTS.md"},
		{"pi", "AGENTS.md"},
		{"omp", "AGENTS.md"},
		{"cursor", "AGENTS.md"},
		{"kimi", "AGENTS.md"},
		{"reasonix", "AGENTS.md"},
		{"dsh", "AGENTS.md"},
		{"dim", "AGENTS.md"},
		{"zeroclaw", "AGENTS.md"},
		{"kiro", "AGENTS.md"},
		{"antigravity", "AGENTS.md"},
		{"muse", "AGENTS.md"},
		{"qwen", "QWEN.md"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, tc.filename)
			const userContent = "# User file\n\ndon't touch this\n"
			if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			// Inject through the production path so cleanup runs against
			// the same wire format the agent saw.
			if _, err := InjectRuntimeConfig(dir, tc.provider, TaskContextForEnv{
				IssueID:          "11111111-2222-3333-4444-555555555555",
				WorkspaceContext: "Issue {{.IssueID}}",
			}); err != nil {
				t.Fatalf("InjectRuntimeConfig: %v", err)
			}
			if err := CleanupRuntimeConfig(dir, tc.provider); err != nil {
				t.Fatalf("CleanupRuntimeConfig: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			s := string(got)
			if strings.Contains(s, runtimeMarkerBegin) || strings.Contains(s, runtimeMarkerEnd) {
				t.Errorf("[%s] marker block must be removed from %s, got:\n%s", tc.provider, tc.filename, s)
			}
			if s != userContent {
				t.Errorf("[%s] user content in %s must be preserved byte-for-byte\n got:\n%q\nwant:\n%q", tc.provider, tc.filename, s, userContent)
			}
		})
	}
}

// Inject → Cleanup → manual edit → Inject must converge back to the
// pre-injection state on the next Cleanup. This is the end-to-end
// regression that locks in: the user's repo is byte-identical to what
// they had before the task, every task cycle.
func TestInjectThenCleanupRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const userContent = "# User-authored CLAUDE.md\n\n- rule A\n- rule B\n"
	if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Two full inject→cleanup cycles — covers both the "first task on a
	// fresh user file" path and the "subsequent task hits a clean file
	// again" path.
	for i := 0; i < 2; i++ {
		if _, err := InjectRuntimeConfig(dir, "claude", TaskContextForEnv{
			IssueID:          "11111111-2222-3333-4444-555555555555",
			WorkspaceContext: "Issue {{.IssueID}}",
		}); err != nil {
			t.Fatalf("iter %d inject: %v", i, err)
		}
		if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
			t.Fatalf("iter %d cleanup: %v", i, err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("iter %d read back: %v", i, err)
		}
		if string(got) != userContent {
			t.Errorf("iter %d: user file must be byte-identical to pre-injection state\n got:\n%q\nwant:\n%q", i, string(got), userContent)
		}
	}
}

// Byte-exact boundary coverage flagged in PR #3438 review (Elon): the
// previous cleanup used TrimRight + "\n" and TrimSpace-based file removal,
// which created a real diff in three boundary cases. The table walks each
// one through a full inject→cleanup cycle and asserts the file ends up
// byte-identical (or, for missing-file, that it stays missing).
func TestInjectThenCleanupRoundTripByteExactBoundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		// seed describes the pre-inject filesystem state. When seedExists
		// is false the file is absent; when true the file is created with
		// seedContent (which may be empty / whitespace-only / arbitrary
		// bytes).
		seedExists  bool
		seedContent string
	}{
		{
			name:        "file missing — Inject creates, Cleanup removes",
			seedExists:  false,
			seedContent: "",
		},
		{
			name:        "pre-existing empty file (zero bytes)",
			seedExists:  true,
			seedContent: "",
		},
		{
			name:        "pre-existing whitespace-only file",
			seedExists:  true,
			seedContent: "   \n",
		},
		{
			name:        "no trailing newline",
			seedExists:  true,
			seedContent: "rules",
		},
		{
			name:        "one trailing newline (the common markdown shape)",
			seedExists:  true,
			seedContent: "# Rules\n\nbody\n",
		},
		{
			name:        "two trailing newlines",
			seedExists:  true,
			seedContent: "rules\n\n",
		},
		{
			name:        "many trailing newlines",
			seedExists:  true,
			seedContent: "rules\n\n\n\n",
		},
		{
			name:        "CRLF line endings",
			seedExists:  true,
			seedContent: "rule A\r\nrule B\r\n",
		},
		{
			name:        "no final newline AND embedded blank lines",
			seedExists:  true,
			seedContent: "para 1\n\npara 2\n\npara 3",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "CLAUDE.md")

			if tc.seedExists {
				if err := os.WriteFile(path, []byte(tc.seedContent), 0o644); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}

			// Two cycles to cover both "first inject hits user file" and
			// "subsequent inject hits a cleaned file" paths.
			for i := 0; i < 2; i++ {
				if _, err := InjectRuntimeConfig(dir, "claude", TaskContextForEnv{
					IssueID:          "11111111-2222-3333-4444-555555555555",
					WorkspaceContext: "Issue {{.IssueID}}",
				}); err != nil {
					t.Fatalf("iter %d inject: %v", i, err)
				}
				if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
					t.Fatalf("iter %d cleanup: %v", i, err)
				}

				if !tc.seedExists {
					// Missing file must remain missing after the cycle so
					// the user's directory listing is also byte-identical
					// (no zero-byte stub left behind).
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Errorf("iter %d: file must remain missing, stat err=%v", i, err)
					}
					continue
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("iter %d read back: %v", i, err)
				}
				if string(got) != tc.seedContent {
					t.Errorf("iter %d: file must be byte-identical to seed\n got:  %q\n want: %q", i, string(got), tc.seedContent)
				}
			}
		})
	}
}

// Markdown formatters collapse the blank lines before an HTML block to one
// and drop leading blank lines entirely. lfFormatter also rewrites CRLF to LF
// (Prettier's default endOfLine); crlfFormatter keeps the file's CRLF style
// and applies it to the whole file, including the managed block.
func lfFormatter(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimLeft(s, "\n")
}

func crlfFormatter(s string) string {
	return strings.ReplaceAll(lfFormatter(s), "\n", "\r\n")
}

func TestCleanupRuntimeConfigAfterFormatting(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, seed string
		format     func(string) string
		want       string
	}{
		{"no newline", "rules", lfFormatter, "rules"},
		{"one newline", "rules\n", lfFormatter, "rules\n"},
		{"two newlines", "rules\n\n", lfFormatter, "rules\n\n"},
		{"multi-line", "# Rules\n\n- a\n- b\n", lfFormatter, "# Rules\n\n- a\n- b\n"},
		{"empty file", "", lfFormatter, ""},
		{"blank file", "\n\n", lfFormatter, "\n\n"},
		// The formatter rewrote the user's line endings itself; the restored
		// boundary follows that style instead of leaving a stray CRLF.
		{"crlf to lf", "a\r\nb\r\n", lfFormatter, "a\nb\n"},
		{"crlf preserved", "a\r\nb\r\n", crlfFormatter, "a\r\nb\r\n"},
		{"crlf two newlines preserved", "a\r\n\r\n", crlfFormatter, "a\r\n\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "CLAUDE.md")
			if err := os.WriteFile(path, []byte(tc.seed), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := InjectRuntimeConfig(dir, "claude", TaskContextForEnv{}); err != nil {
				t.Fatal(err)
			}
			injected, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.format(string(injected))), 0o644); err != nil {
				t.Fatal(err)
			}
			// Re-inject after formatting: metadata must survive the replace path.
			if _, err := InjectRuntimeConfig(dir, "claude", TaskContextForEnv{}); err != nil {
				t.Fatal(err)
			}
			if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("file must survive cleanup: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("cleanup changed user content after formatting: got %q, want %q", got, tc.want)
			}
		})
	}
}

// Idempotency across the byte-exact boundaries: when a second Inject runs
// against a file that already carries a marker block (the "replace in
// place" branch), the surrounding bytes must stay untouched and the
// subsequent Cleanup must still restore the user's original file
// byte-exactly. This guards against a regression where the replace path
// would re-normalise pre/post bytes the way the old cleanup did.
func TestInjectReplaceThenCleanupRestoresByteExact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		seedContent string
	}{
		{name: "no trailing newline", seedContent: "rules"},
		{name: "two trailing newlines", seedContent: "rules\n\n"},
		{name: "empty file", seedContent: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "CLAUDE.md")
			if err := os.WriteFile(path, []byte(tc.seedContent), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			// First inject — append path.
			if _, err := InjectRuntimeConfig(dir, "claude", TaskContextForEnv{
				IssueID:          "11111111-2222-3333-4444-555555555555",
				WorkspaceContext: "Issue {{.IssueID}}",
			}); err != nil {
				t.Fatalf("first inject: %v", err)
			}
			// Second inject — replace-in-place path.
			if _, err := InjectRuntimeConfig(dir, "claude", TaskContextForEnv{
				IssueID:          "11111111-2222-3333-4444-555555555555",
				WorkspaceContext: "Issue {{.IssueID}}",
			}); err != nil {
				t.Fatalf("second inject: %v", err)
			}
			if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
				t.Fatalf("cleanup: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(got) != tc.seedContent {
				t.Errorf("file must be byte-identical to seed after replace+cleanup\n got:  %q\n want: %q", string(got), tc.seedContent)
			}
		})
	}
}

func TestCleanupRuntimeConfigPreservesEditsAfterFormatting(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, seed, prefix, suffix, want string
	}{
		{"edited body", "rules\n", "updated rules\n\n", "", "updated rules\n"},
		{"added suffix", "rules\n", "rules\n\n", "\nnew section\n", "rules\n\nnew section\n"},
		{"edited boundary", "rules\n", "rules\n\n\n\n", "", "rules\n\n"},
		// An empty prefix only means "blank original" when the metadata says
		// so. Here the agent deleted the original body (and may have written
		// a replacement after the block); the old trailing newline must not
		// come back in front of the new content.
		{"body replaced after block", "# Old\n", "", "# New\n", "# New\n"},
		{"body deleted", "# Old\n", "", "", ""},
		{"blank file gains content after block", "", "", "# New\n", "# New\n"},
		// Newline-only originals: the formatter dropped the leading blank
		// lines and the agent wrote a body after the block. The recorded
		// newlines must not be restored in front of the new body.
		{"newline-only file gains content after block", "\n", "", "# New\n", "# New\n"},
		{"crlf-only file gains content after block", "\r\n", "", "# New\r\n", "# New\r\n"},
		{"newline-only file left blank", "\n", "", "", "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "CLAUDE.md")
			if err := os.WriteFile(path, []byte(tc.seed), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := writeRuntimeConfigFile(path, "brief"); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			start, end, found := locateMarkerBlock(string(data))
			if !found {
				t.Fatal("missing block")
			}
			if err := os.WriteFile(path, []byte(tc.prefix+string(data[start:end])+tc.suffix), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("pre-existing file must survive cleanup: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseRuntimeBoundaryMeta(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		metadata string
		ok       bool
		want     runtimeBoundaryMeta
	}{
		{`"\n" -->` + "\n", true, runtimeBoundaryMeta{boundary: "\n"}},
		{`"\r\n\r\n" -->` + "\n", true, runtimeBoundaryMeta{boundary: "\r\n\r\n"}},
		{`"" blank -->` + "\n", true, runtimeBoundaryMeta{boundary: "", blank: true}},
		{`"\n" blank -->` + "\r\n", true, runtimeBoundaryMeta{boundary: "\n", blank: true}},
		{`"text" -->`, false, runtimeBoundaryMeta{}},
		{`"\n"`, false, runtimeBoundaryMeta{}},
		{`"\n" bogus -->`, false, runtimeBoundaryMeta{}},
		{`invalid -->`, false, runtimeBoundaryMeta{}},
	} {
		block := runtimeMarkerBegin + "\n" + runtimeBoundaryPrefix + tc.metadata
		got, ok := parseRuntimeBoundaryMeta(block)
		if ok != tc.ok || got != tc.want {
			t.Errorf("metadata %q: got (%+v, %v), want (%+v, %v)", tc.metadata, got, ok, tc.want, tc.ok)
		}
	}
}

// A daemon that predates boundary metadata strips the metadata line together
// with the block, and a current daemon cleaning a legacy block (no metadata)
// keeps the fixed-separator behaviour, including deleting files it created.
func TestCleanupRuntimeConfigLegacyBlockCompat(t *testing.T) {
	t.Parallel()
	legacyBlock := runtimeMarkerBegin + "\nbrief\n" + runtimeMarkerEnd + "\n"
	for _, tc := range []struct {
		name, content string
		want          *string
	}{
		{"legacy appended", "rules\n" + runtimeManagedSeparator + legacyBlock, legacyWant("rules\n")},
		{"legacy created", legacyBlock, nil},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "CLAUDE.md")
		if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if tc.want == nil {
			if !os.IsNotExist(err) {
				t.Errorf("%s: created file must be removed, got %q (err %v)", tc.name, got, err)
			}
			continue
		}
		if err != nil || string(got) != *tc.want {
			t.Errorf("%s: got %q (err %v), want %q", tc.name, got, err, *tc.want)
		}
	}
}

func legacyWant(s string) *string { return &s }

// The fixed managed separator is the invariant that makes byte-exact
// cleanup possible. This test pins it: writeRuntimeConfigFile must
// produce exactly `<user-bytes><\n\n><marker-block>` for ANY non-empty
// or empty pre-existing file, with no trailing-newline normalisation.
func TestWriteRuntimeConfigFileAlwaysInsertsFixedManagedSeparator(t *testing.T) {
	t.Parallel()
	for _, seed := range []string{"", "rules", "rules\n", "rules\n\n", "rules\n\n\n\n"} {
		seed := seed
		t.Run(fmt.Sprintf("seed=%q", seed), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "CLAUDE.md")
			if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if err := writeRuntimeConfigFile(path, "brief body"); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			s := string(got)
			// The seed must appear verbatim at the start of the file —
			// no extra newline appended, no trailing newline trimmed.
			if !strings.HasPrefix(s, seed) {
				t.Errorf("seed bytes must survive verbatim at the start of the file\n got: %q\n seed: %q", s, seed)
			}
			// Immediately after the seed we must see the fixed managed
			// separator, then the begin marker.
			markerStart := len(seed) + len(runtimeManagedSeparator)
			if len(s) < markerStart+len(runtimeMarkerBegin) {
				t.Fatalf("file shorter than expected layout\n got: %q", s)
			}
			if got, want := s[len(seed):markerStart], runtimeManagedSeparator; got != want {
				t.Errorf("expected managed separator %q immediately after seed, got %q", want, got)
			}
			if got, want := s[markerStart:markerStart+len(runtimeMarkerBegin)], runtimeMarkerBegin; got != want {
				t.Errorf("expected begin marker after managed separator, got %q", got)
			}
		})
	}
}

// Cross-thread fan-out moved to the per-turn prompt (MUL-5377).
func TestMultiThreadReplyInstructionsFanOut(t *testing.T) {
	t.Parallel()
	out := BuildMultiThreadCommentReplyInstructions("55555555-6666-7777-8888-999999999999", []ThreadReplyTarget{
		{ThreadID: "c1", ParentID: "c1"},
		{ThreadID: "c2", ParentID: "c2"},
		{ThreadID: "c3", ParentID: "c3"},
	}, false)

	for _, want := range []string{
		"3 DISTINCT threads",
		"Post ONE reply per thread",
		"OVERRIDES",
		"--parent c1", "--parent c2", "--parent c3",
		"OLDEST thread first",
		// MUL-5825: the posting mechanism is a pointer at the brief's
		// canonical section plus the one multi-thread-specific delta.
		"`## Comment Formatting`",
		"DISTINCT body file per thread",
		"never reuse a `--parent` from an earlier turn",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("cross-thread instructions must contain %q, got:\n%s", want, out)
		}
	}

	// Pin ledger (MUL-5825): the embedded file-operations cookbook was
	// retired in favour of the `## Comment Formatting` pointer above — it
	// triple-wrote the mechanism already carried by the brief and the
	// single-thread cookbook (~1KB per multi-thread turn). These strings are
	// the retired machinery; none may reappear in the fan-out block. The
	// `--content-file` / inline `--content` anchors and the semantic
	// `\n`-escape anchor (replacing the phrasing-fragile "Do NOT write
	// literal") were added on Elon's #6517 review: without them, prose-only
	// restatements of the flag mechanics could regrow under green tests.
	for _, banned := range []string{
		"For EACH thread above",                // old cookbook opener
		"UTF-8 file with your file-write tool", // restated mechanism
		"multica issue comment add",            // embedded example commands
		"--content-file",                       // restated posting flag (#6517 review)
		"inline `--content`",                   // restated inline ban (#6517 review)
		"--content-stdin",                      // restated HEREDOC ban
		"rm ./reply-",                          // unix cleanup example
		"Remove-Item",                          // windows cleanup example
		"`\\n` escape",                         // restated \n-escape rule, any phrasing
	} {
		if strings.Contains(out, banned) {
			t.Errorf("fan-out block re-grew retired cookbook text %q (mechanism lives in ## Comment Formatting — MUL-5825), got:\n%s", banned, out)
		}
	}
}

// TestMultiThreadReplyInstructionsOSInvariant pins that the fan-out block is
// byte-identical across host OSes (MUL-5825). The only OS-dependent text was
// the embedded cleanup command pair (`rm` vs `Remove-Item`), which left with
// the cookbook; the OS split now lives solely in the brief's
// `## Comment Formatting`. If this fails, OS-specific mechanism text crept
// back into the block — move it to the brief instead.
//
// Not parallel: mutates the package-level runtimeGOOS.
func TestMultiThreadReplyInstructionsOSInvariant(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })

	targets := []ThreadReplyTarget{
		{ThreadID: "c1", ParentID: "c1"},
		{ThreadID: "c2", ParentID: "c2"},
	}
	for _, leader := range []bool{false, true} {
		runtimeGOOS = "linux"
		linux := BuildMultiThreadCommentReplyInstructions("55555555-6666-7777-8888-999999999999", targets, leader)
		runtimeGOOS = "windows"
		windows := BuildMultiThreadCommentReplyInstructions("55555555-6666-7777-8888-999999999999", targets, leader)
		if linux != windows {
			t.Errorf("fan-out block (leader=%v) must be OS-invariant\nlinux:\n%s\nwindows:\n%s", leader, linux, windows)
		}
	}
}

// Single-thread reply cookbook moved to the per-turn prompt (MUL-5377).
func TestSingleThreadReplyInstructionsKeepSingleParent(t *testing.T) {
	t.Parallel()
	out := BuildCommentReplyInstructions("claude", "55555555-6666-7777-8888-999999999999", "c3", false)

	if strings.Contains(out, "DISTINCT threads") {
		t.Errorf("single/same-thread instructions must not emit the multi-thread fan-out block, got:\n%s", out)
	}
	if !strings.Contains(out, "--parent c3 --content-file ./reply.md") {
		t.Errorf("single/same-thread instructions must keep the single --parent=trigger cookbook, got:\n%s", out)
	}
}
