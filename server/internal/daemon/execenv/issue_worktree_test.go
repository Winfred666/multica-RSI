package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func issueWorktreeTestParams(t *testing.T, repo, issueID, base string) LocalWorktreeParams {
	t.Helper()
	return LocalWorktreeParams{
		LocalPath: repo, EnvRoot: t.TempDir(), TaskID: turnOneTask,
		WorkspaceID: testBranchOwner.WorkspaceID, AgentID: testBranchOwner.AgentID,
		IssueID: issueID, ConversationKey: "MUL-100", BaseBranch: base,
	}
}

func TestPrepareIssueBranchWithoutAgent(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	origin := t.TempDir()
	gitRun(t, origin, "init", "--bare", "-b", "main")
	gitRun(t, repo, "remote", "add", "origin", origin)
	params := issueWorktreeTestParams(t, repo, "root", "main")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user WIP\n")
	before := gitRun(t, repo, "status", "--porcelain")
	branch, err := PrepareIssueBranch(params, worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if gitRun(t, repo, "rev-parse", branch) != gitRun(t, repo, "rev-parse", "main") {
		t.Fatal("branch was not created from clean main")
	}
	if gitRun(t, origin, "rev-parse", branch) != gitRun(t, repo, "rev-parse", branch) {
		t.Fatal("branch reservation was not published to origin")
	}
	if gitRun(t, repo, "status", "--porcelain") != before || gitRun(t, repo, "branch", "--show-current") != "main" {
		t.Fatal("reserving an issue branch changed the user's checkout")
	}
	params.IssueBranch = branch
	wt, err := PrepareLocalWorktree(params, worktreeTestLogger())
	if err != nil {
		t.Fatalf("first task could not continue the reserved branch: %v", err)
	}
	t.Cleanup(func() { finalizeOK(t, wt) })
	writeFile(t, filepath.Join(wt.Path, "result.txt"), "useful work\n")
	gitRun(t, wt.Path, "add", ".")
	gitRun(t, wt.Path, "commit", "-m", "result")
	tip := gitRun(t, repo, "rev-parse", branch)
	if again, err := PrepareIssueBranch(params, worktreeTestLogger()); err != nil || again != branch {
		t.Fatalf("reservation while a task is running: %s, %v", again, err)
	}
	if gitRun(t, repo, "rev-parse", branch) != tip {
		t.Fatal("reservation reset existing work")
	}
	other := params
	other.WorkspaceID = "another-workspace"
	if _, err := PrepareIssueBranch(other, worktreeTestLogger()); err == nil {
		t.Fatal("another workspace took ownership of the branch")
	}
}

func TestIssueWorktreeHierarchy(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	gitRun(t, repo, "checkout", "-b", "unrelated")
	writeFile(t, filepath.Join(repo, "unrelated.txt"), "wrong scope\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "unrelated change")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "uncommitted wrong scope\n")
	before := gitRun(t, repo, "status", "--porcelain")

	base := ""
	var worktrees []*LocalWorktree
	for level, issueID := range []string{"root", "child", "grandchild"} {
		params := issueWorktreeTestParams(t, repo, issueID, base)
		wt, err := PrepareLocalWorktree(params, worktreeTestLogger())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { finalizeOK(t, wt) })
		if _, err := os.Stat(filepath.Join(wt.Path, "unrelated.txt")); !os.IsNotExist(err) {
			t.Fatalf("level %d inherited unrelated checkout: %v", level, err)
		}
		if got := readFile(t, filepath.Join(wt.Path, "tracked.txt")); got != "original\n" {
			t.Fatalf("level %d inherited seed WIP: %q", level, got)
		}
		for _, ancestor := range []string{"root", "child"}[:level] {
			if got := readFile(t, filepath.Join(wt.Path, ancestor+".txt")); got != ancestor {
				t.Fatalf("missing ancestor %s: %q", ancestor, got)
			}
		}
		writeFile(t, filepath.Join(wt.Path, issueID+".txt"), issueID)
		gitRun(t, wt.Path, "add", ".")
		gitRun(t, wt.Path, "commit", "-m", issueID)
		base = wt.Branch
		worktrees = append(worktrees, wt)
	}
	if gitRun(t, repo, "status", "--porcelain") != before || gitRun(t, repo, "branch", "--show-current") != "unrelated" {
		t.Fatal("preparing issue worktrees changed the user's checkout")
	}
	// The child is forked from its mentor while that mentor is still running.
	if _, err := os.Stat(filepath.Join(worktrees[0].Path, "child.txt")); !os.IsNotExist(err) {
		t.Fatal("child changes leaked back into the parent worktree")
	}
}

func TestIssueWorktreeAcceptsRemoteChildMerge(t *testing.T) {
	t.Parallel()
	repo, origin := newTestRepo(t), t.TempDir()
	gitRun(t, origin, "init", "--bare", "-b", "main")
	gitRun(t, repo, "remote", "add", "origin", origin)
	parent, err := PrepareLocalWorktree(issueWorktreeTestParams(t, repo, "parent", "main"), worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { finalizeOK(t, parent) })
	child, err := PrepareLocalWorktree(issueWorktreeTestParams(t, repo, "child", parent.Branch), worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(child.Path, "child.txt"), "accepted work\n")
	finalizeOK(t, child)
	if _, err := os.Stat(child.Path); !os.IsNotExist(err) {
		t.Fatal("child completion left its worktree behind")
	}
	if gitRun(t, origin, "rev-parse", child.Branch) != gitRun(t, repo, "rev-parse", child.Branch) {
		t.Fatal("child completion did not publish its final commit")
	}

	// Model GitHub's merge commit in a separate clone; the parent stays checked out.
	review := t.TempDir()
	gitRun(t, review, "clone", "--branch", parent.Branch, origin, ".")
	gitRun(t, review, "-c", "user.name=Test", "-c", "user.email=test@example.com", "merge", "--no-ff", "origin/"+child.Branch, "-m", "accept child")
	gitRun(t, review, "push", "origin", "HEAD")
	if _, err := os.Stat(filepath.Join(parent.Path, "child.txt")); !os.IsNotExist(err) {
		t.Fatal("remote acceptance changed the parent's local worktree")
	}
	gitRun(t, parent.Path, "fetch", "origin")
	gitRun(t, parent.Path, "merge", "--no-edit", "origin/"+parent.Branch)
	if got := readFile(t, filepath.Join(parent.Path, "child.txt")); got != "accepted work\n" {
		t.Fatalf("accepted child work = %q", got)
	}
	if problem := parent.CheckDelivery(); problem != nil {
		t.Fatalf("accepting the child broke the parent's delivery: %+v", problem)
	}
}

func TestIssueWorktreePublishFailurePreservesWork(t *testing.T) {
	t.Parallel()
	repo, origin := newTestRepo(t), t.TempDir()
	gitRun(t, origin, "init", "--bare", "-b", "main")
	gitRun(t, repo, "remote", "add", "origin", origin)
	params := issueWorktreeTestParams(t, repo, "child", "main")
	wt, err := PrepareLocalWorktree(params, worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wt.Path, "result.txt"), "preserved work\n")
	gitRun(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing"))
	if outcome, err := wt.Finalize(worktreeTestLogger()); err == nil || outcome.PreservedPath != wt.Path {
		t.Fatalf("failed publication lost the worktree: %+v, %v", outcome, err)
	}
	if got := readFile(t, filepath.Join(wt.Path, "result.txt")); got != "preserved work\n" {
		t.Fatalf("work changed after a failed publication: %q", got)
	}
	gitRun(t, repo, "remote", "set-url", "origin", origin)
	finalizeOK(t, wt)
	if gitRun(t, origin, "show", wt.Branch+":result.txt") != "preserved work" {
		t.Fatal("publication retry lost the completed work")
	}
}

func TestIssueWorktreeBindingSurvivesReadOnlyRunAndReassignment(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	params := issueWorktreeTestParams(t, repo, "root", "main")
	first, err := PrepareLocalWorktree(params, worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	// Production preparation crosses a JSON helper-process boundary.
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var restored LocalWorktree
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	finalizeOK(t, &restored)
	gitRun(t, repo, "rev-parse", "--verify", "refs/heads/"+first.Branch)

	params.EnvRoot = t.TempDir()
	params.IssueBranch = first.Branch
	params.AgentID = "replacement-agent"
	params.AgentName = "Replacement"
	second, err := PrepareLocalWorktree(params, worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer finalizeOK(t, second)
	if second.Branch != first.Branch || !second.Continued || second.BaseBranch != "main" {
		t.Fatalf("binding changed on reassignment: %+v", second)
	}
}

func TestIssueWorktreeBindingFailsClosed(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	params := issueWorktreeTestParams(t, repo, "root", "main")
	first, err := PrepareLocalWorktree(params, worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer finalizeOK(t, first)
	for _, tc := range []struct {
		name, issueID, branch, base, want string
	}{
		{"busy", "root", first.Branch, "main", "cannot check out bound issue branch"},
		{"retry without metadata", "root", "", "main", "cannot check out bound issue branch"},
		{"wrong owner", "other", first.Branch, "main", "not owned by issue"},
		{"missing binding", "root", "missing", "main", "bound issue branch"},
		{"missing parent", "child", "", "missing-parent", "parent branch"},
		{"invalid binding", "root", "bad..branch", "main", "invalid issue branch"},
		{"invalid parent", "child", "", "bad..branch", "invalid parent branch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := issueWorktreeTestParams(t, repo, tc.issueID, tc.base)
			p.IssueBranch = tc.branch
			wt, err := PrepareLocalWorktree(p, worktreeTestLogger())
			if err == nil {
				wt.Discard(worktreeTestLogger())
				t.Fatal("accepted broken issue binding")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
