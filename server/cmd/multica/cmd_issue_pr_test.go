package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func TestLocalPRCompletionRequiresEveryMerge(t *testing.T) {
	for _, tc := range []struct {
		name, otherState, target      string
		disabled, failWrite, wantDone bool
	}{
		{"all merged", "MERGED", "done", false, false, true},
		{"open PR", "OPEN", "done", false, false, false},
		{"closed PR", "CLOSED", "done", false, false, false},
		{"workspace disabled", "MERGED", "none", false, false, false},
		{"issue disabled", "MERGED", "done", true, false, false},
		{"API retry", "MERGED", "done", false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := `{"url":"` + issueTestPRURL + `","headRefName":"issue/child","baseRefName":"issue/parent","headRefOid":"reviewed-head","state":"MERGED"}`
			log := fakeIssueGH(t, response)
			other := strings.Replace(strings.Replace(response, "/pull/1", "/pull/2", 1), `"state":"MERGED"`, `"state":"`+tc.otherState+`"`, 1)
			t.Setenv("GH_TEST_OTHER_PR", other)
			gh := `#!/bin/sh
printf '%s\n' "$*" >> "$GH_TEST_LOG"
case "$3" in
  */pull/2) printf '%s' "$GH_TEST_OTHER_PR" ;;
  *) printf '%s' "$GH_TEST_PR" ;;
esac
`
			if err := os.WriteFile(filepath.Join(filepath.Dir(log), "gh"), []byte(gh), 0700); err != nil {
				t.Fatal(err)
			}
			writes := 0
			failWrite := tc.failWrite
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pull-requests"):
					_ = json.NewEncoder(w).Encode(map[string]any{"pull_requests": []any{map[string]string{"html_url": "https://github.com/owner/repo/pull/2"}},
						"auto_complete": map[string]any{"workspace_enabled": tc.target != "none", "issue_disabled": tc.disabled, "target_status": tc.target}})
				case r.Method == http.MethodGet:
					_ = json.NewEncoder(w).Encode(map[string]any{"parent_issue_id": "parent", "metadata": map[string]string{
						"git_branch": "issue/child", "git_base_branch": "issue/parent", "git_pr_urls": `["` + issueTestPRURL + `"]`}})
				case r.Method == http.MethodPut && r.URL.Path == "/api/issues/"+testIssueUUID:
					if failWrite {
						failWrite = false
						http.Error(w, "retry", http.StatusServiceUnavailable)
						return
					}
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["status"] != "done" {
						t.Errorf("unexpected status update: %v", body)
					}
					writes++
					_, _ = w.Write([]byte(`{}`))
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			defer srv.Close()
			client := cli.NewAPIClient(srv.URL, "workspace", "human-token")
			err := runIssuePullRequestAction(context.Background(), client, testIssueUUID, issuePRDecision{Action: "merge", PR: issueTestPRURL, Head: "reviewed-head"})
			if tc.failWrite {
				if err == nil || writes != 0 {
					t.Fatalf("failed API write was lost: err=%v writes=%d", err, writes)
				}
				err = runIssuePullRequestAction(context.Background(), client, testIssueUUID, issuePRDecision{Action: "merge", PR: issueTestPRURL, Head: "reviewed-head"})
			}
			if err != nil || (writes == 1) != tc.wantDone {
				t.Fatalf("err=%v writes=%d wantDone=%t", err, writes, tc.wantDone)
			}
			calls, _ := os.ReadFile(log)
			if strings.Contains(string(calls), "pr merge") {
				t.Fatal("retry attempted to merge an already merged PR")
			}
		})
	}
}

const issueTestPRURL = "https://github.com/owner/repo/pull/1"

func TestIssuePRReviewReturnsDecisions(t *testing.T) {
	fakeIssueGH(t, `{"url":"`+issueTestPRURL+`","headRefName":"issue/child","baseRefName":"issue/parent","headRefOid":"reviewed-head","state":"OPEN","body":"Acceptance evidence"}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pull-requests") {
			_, _ = w.Write([]byte(`{"pull_requests":[]}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]string{"git_pr_urls": `["` + issueTestPRURL + `"]`}})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace")
	t.Setenv("MULTICA_TOKEN", "human-token")
	cmd := newIssuePRTestCmd()
	_ = cmd.Flags().Set("review", "true")
	out, err := captureStdout(t, func() error { return runIssuePullRequests(cmd, []string{testIssueUUID}) })
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		PRs []struct {
			Head      string            `json:"head_sha"`
			Body      string            `json:"body"`
			Diff      string            `json:"diff"`
			Decisions []issuePRDecision `json:"decisions"`
		} `json:"pull_requests"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.PRs) != 1 {
		t.Fatalf("missing local PR: %s", out)
	}
	pr := result.PRs[0]
	if pr.Head != "reviewed-head" || pr.Body != "Acceptance evidence" || !strings.Contains(pr.Diff, "diff --git") || len(pr.Decisions) != 3 {
		t.Fatalf("incomplete review: %s", out)
	}
	for i, action := range []string{"merge", "resteer", "reject"} {
		if pr.Decisions[i].Action != action || pr.Decisions[i].Head != pr.Head || pr.Decisions[i].PR != issueTestPRURL {
			t.Fatalf("unsafe decision: %+v", pr.Decisions[i])
		}
	}
}

func TestIssuePRDecisionValidation(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"action":"other"}`, `{"action":"resteer","pr":"` + issueTestPRURL + `","head":"sha"}`,
		`{"action":"merge","pr":"` + issueTestPRURL + `","head":"sha","assignee_id":"different"}`,
		`{"action":"merge","pr":"` + issueTestPRURL + `","head":"sha"} {}`,
	} {
		if _, err := parseIssuePRDecision(raw); err == nil {
			t.Errorf("accepted invalid decision %s", raw)
		}
	}
}

func TestMergedIssueBranchCleanup(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		t.Run(map[bool]string{false: "clean", true: "dirty"}[dirty], func(t *testing.T) {
			repo := t.TempDir()
			t.Chdir(repo)
			git := func(args ...string) string {
				t.Helper()
				out, err := exec.Command("git", args...).CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			git("init", "-b", "main")
			git("config", "user.email", "test@example.com")
			git("config", "user.name", "Test")
			git("commit", "--allow-empty", "-m", "initial")
			remote := filepath.Join(t.TempDir(), "remote.git")
			git("init", "--bare", remote)
			git("remote", "add", "origin", remote)
			child := filepath.Join(t.TempDir(), "child")
			git("worktree", "add", "-b", "issue/child", child)
			git("push", "origin", "issue/child")
			if dirty {
				if err := os.WriteFile(filepath.Join(child, "unfinished"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := cleanupMergedIssueBranch(context.Background(), "issue/child")
			if dirty {
				if err == nil {
					t.Fatal("discarded dirty child worktree")
				}
				if _, err := os.Stat(filepath.Join(child, "unfinished")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(child); !os.IsNotExist(err) {
					t.Fatal("child worktree remains")
				}
				if git("branch", "--list", "issue/child") != "" || git("ls-remote", "--heads", "origin", "refs/heads/issue/child") != "" {
					t.Fatal("feature branch remains")
				}
				if err := cleanupMergedIssueBranch(context.Background(), "issue/child"); err != nil {
					t.Fatalf("cleanup retry: %v", err)
				}
			}
		})
	}
}

func newIssuePRTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	for _, flag := range []string{"link", "close", "merge", "head", "decision"} {
		cmd.Flags().String(flag, "", "")
	}
	cmd.Flags().String("output", "json", "")
	cmd.Flags().Bool("review", false, "")
	return cmd
}

func fakeIssueGH(t *testing.T, response string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture requires Unix")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	t.Setenv("GH_TEST_LOG", log)
	t.Setenv("GH_TEST_PR", response)
	t.Setenv("GH_TEST_MERGED", strings.Replace(response, `"state":"OPEN"`, `"state":"MERGED"`, 1))
	t.Setenv("GH_TEST_CLOSED", strings.Replace(response, `"state":"OPEN"`, `"state":"CLOSED"`, 1))
	t.Setenv("GH_TEST_STATE", filepath.Join(dir, "state"))
	t.Setenv("GH_TEST_BRANCH", "issue/parent")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$GH_TEST_LOG"
case "$1 $2" in
  'pr view')
    if [ -f "$GH_TEST_STATE" ]; then cat "$GH_TEST_STATE"; else printf '%s' "$GH_TEST_PR"; fi ;;
  'pr merge') printf '%s' "$GH_TEST_MERGED" > "$GH_TEST_STATE" ;;
  'pr close') printf '%s' "$GH_TEST_CLOSED" > "$GH_TEST_STATE" ;;
  'pr diff') printf '%s' 'diff --git a/solution.py b/solution.py' ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	git := "#!/bin/sh\nprintf '%s\\n' \"git $*\" >> \"$GH_TEST_LOG\"\nif [ \"$1\" = branch ]; then printf '%s' \"${GH_TEST_BRANCH-issue/parent}\"; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(git), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestIssuePullRequestsReviewLocally(t *testing.T) {
	for _, action := range []string{"close", "merge", "resteer"} {
		for _, tc := range []struct {
			name, head, task, parent, prs string
			wantErr                       bool
		}{
			{"member", "reviewed-head", "", "parent", issueTestPRURL, false},
			{"draft PR", "reviewed-head", "", "parent", issueTestPRURL, false},
			{"root member", "reviewed-head", "", "", issueTestPRURL, false},
			{"parent run", "reviewed-head", "parent-run", "parent", issueTestPRURL, false},
			{"wrong run", "reviewed-head", "child-run", "parent", issueTestPRURL, true},
			{"root agent run", "reviewed-head", "parent-run", "", issueTestPRURL, true},
			{"changed head", "old-head", "", "parent", issueTestPRURL, true},
			{"unlinked PR", "reviewed-head", "", "parent", "", true},
			{"wrong checkout", "reviewed-head", "", "parent", issueTestPRURL, action == "merge"},
		} {
			t.Run(action+"/"+tc.name, func(t *testing.T) {
				log := fakeIssueGH(t, `{"url":"`+issueTestPRURL+`","headRefName":"issue/child","baseRefName":"issue/parent","headRefOid":"reviewed-head","state":"OPEN"}`)
				if tc.name == "draft PR" {
					t.Setenv("GH_TEST_PR", strings.TrimSuffix(os.Getenv("GH_TEST_PR"), "}")+`,"isDraft":true}`)
				}
				if tc.name == "wrong checkout" {
					t.Setenv("GH_TEST_BRANCH", "issue/other")
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPut && (action == "close" || action == "resteer") && r.URL.Path == "/api/issues/"+testIssueUUID {
						var body map[string]string
						_ = json.NewDecoder(r.Body).Decode(&body)
						wantStatus := "cancelled"
						if action == "resteer" {
							wantStatus = "in_progress"
						}
						if body["status"] != wantStatus || (action == "resteer" && body["handoff_note"] != "Fix only the assigned module") {
							t.Errorf("unexpected review handoff: %v", body)
						}
						_, _ = w.Write([]byte(`{}`))
						return
					}
					if r.Method != http.MethodGet {
						t.Errorf("GitHub mutations must run locally, got %s %s", r.Method, r.URL.Path)
					}
					switch r.URL.Path {
					case "/api/issues/" + testIssueUUID:
						_ = json.NewEncoder(w).Encode(map[string]any{"parent_issue_id": tc.parent, "metadata": map[string]string{"git_branch": "issue/child", "git_base_branch": "issue/parent"}})
					case "/api/issues/parent/task-runs":
						_, _ = w.Write([]byte(`[{"id":"parent-run"}]`))
					case "/api/issues/" + testIssueUUID + "/pull-requests":
						_ = json.NewEncoder(w).Encode(map[string]any{"pull_requests": []any{map[string]string{"html_url": tc.prs, "branch": "issue/child"}}})
					default:
						t.Errorf("unexpected request %s", r.URL.Path)
						http.NotFound(w, r)
					}
				}))
				defer srv.Close()
				t.Setenv("MULTICA_SERVER_URL", srv.URL)
				t.Setenv("MULTICA_WORKSPACE_ID", "workspace")
				t.Setenv("MULTICA_TOKEN", "mat_test-token")
				t.Setenv("MULTICA_AGENT_ID", "")
				t.Setenv("MULTICA_TASK_ID", tc.task)
				if tc.task == "" {
					t.Setenv("MULTICA_TOKEN", "test-token")
				}
				cmd := newIssuePRTestCmd()
				if action == "resteer" {
					data, _ := json.Marshal(issuePRDecision{Action: "resteer", PR: issueTestPRURL, Head: tc.head, Prompt: "Fix only the assigned module"})
					_ = cmd.Flags().Set("decision", string(data))
				} else {
					_ = cmd.Flags().Set(action, issueTestPRURL)
					_ = cmd.Flags().Set("head", tc.head)
				}
				_, err := captureStdout(t, func() error { return runIssuePullRequests(cmd, []string{testIssueUUID}) })
				if (err != nil) != tc.wantErr {
					t.Fatalf("error = %v, want error %t", err, tc.wantErr)
				}
				calls, _ := os.ReadFile(log)
				mutation := "pr " + action + " " + issueTestPRURL
				if action == "resteer" {
					mutation = "pr comment " + issueTestPRURL + " --body Fix only the assigned module"
				}
				if action == "merge" {
					mutation += " --merge --match-head-commit reviewed-head"

				}
				if strings.Contains(string(calls), mutation+"\n") == tc.wantErr {
					t.Fatalf("unexpected local gh mutation: %s", calls)
				}
				if strings.Contains(string(calls), "pr ready "+issueTestPRURL+"\n") != (tc.name == "draft PR" && action == "merge") {
					t.Fatalf("unexpected draft preparation: %s", calls)
				}
				wantSync := !tc.wantErr && action == "merge" && tc.parent != ""
				if strings.Contains(string(calls), "git fetch origin refs/heads/issue/parent\ngit merge --no-edit FETCH_HEAD\n") != wantSync {
					t.Fatalf("unexpected parent checkout update: %s", calls)
				}
			})
		}
	}
}

func TestIssuePullRequestsFlags(t *testing.T) {
	for _, flags := range []map[string]string{
		{"close": issueTestPRURL},
		{"merge": issueTestPRURL},
		{"head": "reviewed-head"},
		{"link": issueTestPRURL, "head": "reviewed-head"},
		{"link": issueTestPRURL, "close": issueTestPRURL, "head": "reviewed-head"},
		{"merge": issueTestPRURL, "close": issueTestPRURL, "head": "reviewed-head"},
	} {
		cmd := newIssuePRTestCmd()
		for key, value := range flags {
			_ = cmd.Flags().Set(key, value)
		}
		if err := runIssuePullRequests(cmd, []string{testIssueUUID}); err == nil {
			t.Fatalf("accepted invalid flags %v", flags)
		}
	}
}

func TestIssuePullRequestsLinkFailures(t *testing.T) {
	for _, failure := range []string{"malformed gh JSON", "incomplete PR", "wrong PR URL", "gh failure", "metadata write", "PR metadata write"} {
		t.Run(failure, func(t *testing.T) {
			response := `{"url":"` + issueTestPRURL + `","headRefName":"issue/child","baseRefName":"main","headRefOid":"reviewed-head","state":"OPEN"}`
			switch failure {
			case "malformed gh JSON":
				response = `{"url":`
			case "incomplete PR":
				response = `{}`
			case "wrong PR URL":
				response = strings.Replace(response, "/pull/1", "/pull/2", 1)
			}
			log := fakeIssueGH(t, response)
			if failure == "gh failure" {
				if err := os.WriteFile(filepath.Join(filepath.Dir(log), "gh"), []byte("#!/bin/sh\nexit 9\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			posts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					_, _ = w.Write([]byte(`{"metadata":{}}`))
				case http.MethodPut:
					if failure == "metadata write" || (failure == "PR metadata write" && strings.HasSuffix(r.URL.Path, "/git_pr_urls")) {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					if failure != "PR metadata write" {
						t.Error("malformed local PR changed issue metadata")
					}
					_, _ = w.Write([]byte(`{}`))
				case http.MethodPost:
					posts++
					http.Error(w, "not mirrored", http.StatusNotFound)
				}
			}))
			defer srv.Close()
			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "workspace")
			t.Setenv("MULTICA_TOKEN", "test-token")
			cmd := newIssuePRTestCmd()
			_ = cmd.Flags().Set("link", issueTestPRURL)
			err := runIssuePullRequests(cmd, []string{testIssueUUID})
			if err == nil || posts != 0 {
				t.Fatalf("err=%v posts=%d", err, posts)
			}
		})
	}
}

func TestIssuePullRequestsLinkBindsBranchFirst(t *testing.T) {
	for _, tc := range []struct {
		name, branch, base, parent, prBase string
		wantWrites                         []string
		wantErr                            bool
	}{
		{"existing binding", "issue/child", "issue/parent", "parent", "issue/parent", nil, false},
		{"existing child PR", "", "", "parent", "issue/parent", []string{"git_base_branch", "git_branch"}, false},
		{"existing root PR", "", "", "", "main", []string{"git_base_branch", "git_branch"}, false},
		{"partial binding", "issue/child", "", "parent", "issue/parent", []string{"git_base_branch"}, false},
		{"wrong branch", "issue/sibling", "issue/parent", "parent", "issue/parent", nil, true},
		{"wrong parent base", "", "", "parent", "main", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeIssueGH(t, `{"url":"`+issueTestPRURL+`","headRefName":"issue/child","baseRefName":"`+tc.prBase+`","headRefOid":"reviewed-head","state":"OPEN"}`)
			var writes []string
			linked := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+testIssueUUID:
					_ = json.NewEncoder(w).Encode(map[string]any{"parent_issue_id": tc.parent, "metadata": map[string]string{"git_branch": tc.branch, "git_base_branch": tc.base}})
				case r.Method == http.MethodGet && r.URL.Path == "/api/issues/parent":
					_, _ = w.Write([]byte(`{"metadata":{"git_branch":"issue/parent"}}`))
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pull-requests"):
					_, _ = w.Write([]byte(`{"pull_requests":[]}`))
				case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/metadata/git_pr_urls"):
					if !reflect.DeepEqual(writes, tc.wantWrites) {
						t.Errorf("linked before binding metadata: %v", writes)
					}
					var body struct{ Value string }
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body.Value != `["`+issueTestPRURL+`"]` {
						t.Errorf("unexpected PR binding: %v", body)
					}
					linked = true
					_, _ = w.Write([]byte(`{}`))
				case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/metadata/"):
					key := filepath.Base(r.URL.Path)
					var body struct{ Value string }
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					want := "issue/child"
					if key == "git_base_branch" {
						want = tc.prBase
					}
					if body.Value != want {
						t.Errorf("%s = %q, want %q", key, body.Value, want)
					}
					writes = append(writes, key)
					_, _ = w.Write([]byte(`{}`))
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "workspace")
			t.Setenv("MULTICA_TOKEN", "test-token")
			cmd := newIssuePRTestCmd()
			_ = cmd.Flags().Set("link", issueTestPRURL)
			_, err := captureStdout(t, func() error { return runIssuePullRequests(cmd, []string{testIssueUUID}) })
			if (err != nil) != tc.wantErr || linked == tc.wantErr || !reflect.DeepEqual(writes, tc.wantWrites) {
				t.Fatalf("err=%v linked=%t metadata writes=%v", err, linked, writes)
			}
		})
	}
}
