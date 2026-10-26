package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIssueReviewRequiresLinkedBranchPR(t *testing.T) {
	for _, command := range []string{"status", "update"} {
		for _, tc := range []struct {
			name, branch, prs, status string
			readFails, wantBlocked    bool
		}{
			{"no branch", "", `[]`, "in_review", false, false},
			{"missing PR", "issue/mul-1", `[]`, "in_review", false, true},
			{"wrong branch PR", "issue/mul-1", `[{"html_url":"https://github.com/org/repo/pull/1","branch":"another"}]`, "in_review", false, true},
			{"missing URL", "issue/mul-1", `[{"branch":"issue/mul-1"}]`, "in_review", false, true},
			{"linked PR", "issue/mul-1", `[{"html_url":"https://github.com/org/repo/pull/1","branch":"issue/mul-1"}]`, "in_review", false, false},
			{"other status", "issue/mul-1", `[]`, "in_progress", false, false},
			{"PR read fails", "issue/mul-1", `[]`, "in_review", true, true},
		} {
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				updates := 0
				prReads := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+testIssueUUID:
						_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]string{"git_branch": tc.branch}})
					case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pull-requests"):
						prReads++
						if tc.readFails {
							http.Error(w, "unavailable", http.StatusServiceUnavailable)
							return
						}
						_, _ = w.Write([]byte(`{"pull_requests":` + tc.prs + `}`))
					case r.Method == http.MethodPut && r.URL.Path == "/api/issues/"+testIssueUUID:
						updates++
						_, _ = w.Write([]byte(`{"id":"` + testIssueUUID + `"}`))
					default:
						t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
						http.NotFound(w, r)
					}
				}))
				defer srv.Close()
				t.Setenv("MULTICA_SERVER_URL", srv.URL)
				t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
				t.Setenv("MULTICA_TOKEN", "test-token")
				_, err := captureStdout(t, func() error {
					if command == "status" {
						return runIssueStatus(newIssueStatusTestCmd(), []string{testIssueUUID, tc.status})
					}
					cmd := newIssueUpdateTestCmd()
					_ = cmd.Flags().Set("status", tc.status)
					return runIssueUpdate(cmd, []string{testIssueUUID})
				})
				if tc.wantBlocked {
					if err == nil || updates != 0 {
						t.Fatalf("review wasn't blocked: err=%v, updates=%d", err, updates)
					}
					if !tc.readFails && !strings.Contains(err.Error(), "link your branch's PR to this issue") {
						t.Fatalf("missing actionable review error: %v", err)
					}
				} else if err != nil || updates != 1 {
					t.Fatalf("valid update failed: err=%v, updates=%d", err, updates)
				}
				if (tc.branch == "" || tc.status != "in_review") && prReads != 0 {
					t.Fatal("unbound issue or non-review update fetched PRs")
				}
			})
		}
	}
}
