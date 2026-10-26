package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const issueTestPRURL = "https://github.com/owner/repo/pull/1"

func newIssuePRTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	for _, flag := range []string{"link", "close", "merge", "head"} {
		cmd.Flags().String(flag, "", "")
	}
	cmd.Flags().String("output", "json", "")
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
	t.Setenv("GH_TEST_BRANCH", "issue/parent")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$GH_TEST_LOG\"\nif [ \"$1 $2\" = 'pr view' ]; then printf '%s' \"$GH_TEST_PR\"; fi\n"
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
	for _, action := range []string{"close", "merge"} {
		for _, tc := range []struct {
			name, head, task, parent, prs string
			wantErr                       bool
		}{
			{"member", "reviewed-head", "", "parent", issueTestPRURL, false},
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
				if tc.name == "wrong checkout" {
					t.Setenv("GH_TEST_BRANCH", "issue/other")
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				_ = cmd.Flags().Set(action, issueTestPRURL)
				_ = cmd.Flags().Set("head", tc.head)
				_, err := captureStdout(t, func() error { return runIssuePullRequests(cmd, []string{testIssueUUID}) })
				if (err != nil) != tc.wantErr {
					t.Fatalf("error = %v, want error %t", err, tc.wantErr)
				}
				calls, _ := os.ReadFile(log)
				mutation := "pr " + action + " " + issueTestPRURL
				if action == "merge" {
					mutation += " --merge --match-head-commit reviewed-head"
					if tc.parent != "" {
						mutation += " --delete-branch"
					}
				}
				if strings.Contains(string(calls), mutation+"\n") == tc.wantErr {
					t.Fatalf("unexpected local gh mutation: %s", calls)
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
	for _, failure := range []string{"malformed gh JSON", "incomplete PR", "wrong PR URL", "gh failure", "metadata write", "webhook pending"} {
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
					if failure == "metadata write" {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					if failure != "webhook pending" {
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
			if err == nil || (failure != "webhook pending" && posts != 0) {
				t.Fatalf("err=%v posts=%d", err, posts)
			}
			if failure == "webhook pending" && !strings.Contains(err.Error(), "confirm GitHub App pull_request webhooks") {
				t.Fatalf("missing webhook recovery instructions: %v", err)
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
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pull-requests"):
					if !reflect.DeepEqual(writes, tc.wantWrites) {
						t.Errorf("linked before binding metadata: %v", writes)
					}
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if !reflect.DeepEqual(body, map[string]any{"url": issueTestPRURL}) {
						t.Errorf("unexpected link request: %v", body)
					}
					linked = true
					_, _ = w.Write([]byte(`{"pull_requests":[]}`))
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
