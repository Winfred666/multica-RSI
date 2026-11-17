package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRemoteSOPTaskDeliveryRunsTargetCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	for _, terminal := range []string{"complete", "fail"} {
		t.Run(terminal, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "multica")
			t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("FIXTURE_TERMINAL", terminal)
			writeTestExecutable(t, binary, []byte(`#!/usr/bin/env python3
import os, sys
assert sys.argv[1:] == ['sop', 'run', 'inspect', '--help', 'two words', '', '$(literal)']
assert os.environ['MULTICA_AGENT_ID'] == 'assignee'
assert os.environ['MULTICA_ISSUE_ID'] == 'issue'
assert os.environ['MULTICA_WORKSPACE_ID'] == 'workspace'
assert os.environ['MULTICA_TOKEN'] == 'pat_owner'
print(os.getcwd())
print('stderr text', file=sys.stderr)
sys.exit(7 if os.environ['FIXTURE_TERMINAL'] == 'fail' else 0)
`))
			cwd := t.TempDir()
			task := Task{ID: "task", AgentID: "assignee", RuntimeID: "runtime", IssueID: "issue", WorkspaceID: "workspace", PriorWorkDir: t.TempDir(), AuthToken: "mat_scoped", Agent: &AgentData{ID: "assignee"},
				TriggerCommentID: "answer", TriggerCommentContent: `!sop-remote inspect {"args":["--help","two words","","$(literal)"]}`}
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.URL.Path)
				switch r.URL.Path {
				case "/api/daemon/runtimes/runtime/tasks/claim":
					_ = json.NewEncoder(w).Encode(map[string]any{"task": task})
				case "/api/issues/issue":
					state, _ := json.Marshal(map[string]string{"workdir": cwd})
					_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]string{"sop.inspect": string(state)}})
					return
				case "/api/issues/issue/comments":
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					if terminal != "fail" || !strings.HasPrefix(body["content"], "/note SoP command failed:") || r.Header.Get("Authorization") != "Bearer pat_owner" {
						t.Errorf("exception comment=%v", body)
					}
				case "/api/daemon/tasks/task/messages":
					var batch struct {
						Messages []TaskMessageData `json:"messages"`
					}
					_ = json.NewDecoder(r.Body).Decode(&batch)
					if len(batch.Messages) != 2 || batch.Messages[0].Seq != 1 || batch.Messages[1].CallID != "answer" || !strings.Contains(batch.Messages[1].Output, cwd) || !strings.Contains(batch.Messages[1].Output, "stderr text") {
						t.Errorf("messages=%+v", batch.Messages)
					}
				case "/api/comments/answer/keep-replies":
					if terminal != "complete" || r.Method != "DELETE" || calls[len(calls)-2] != "/api/daemon/tasks/task/complete" ||
						r.Header.Get("Authorization") != "Bearer pat_owner" || r.Header.Get("X-Workspace-ID") != "workspace" {
						t.Error("answer must be consumed only after successful completion")
					}
				case "/api/daemon/tasks/task/start", "/api/daemon/tasks/task/progress", "/api/daemon/tasks/task/" + terminal, "/api/daemon/tasks/task/status":
				default:
					t.Errorf("unexpected API (poll or model): %s", r.URL.Path)
				}
				_, _ = w.Write([]byte(`{"status":"running"}`))
			}))
			defer server.Close()
			d := &Daemon{client: NewClient(server.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil)), activeEnvRoots: map[string]int{}, runtimeIndex: map[string]Runtime{"runtime": {Provider: "no-model"}}, cfg: Config{ServerBaseURL: server.URL, WorkspacesRoot: t.TempDir()}}
			d.client.SetToken("pat_owner")
			d.runner = taskRunnerFunc(d.runTask)
			d.handleHeartbeatActions(t.Context(), "runtime", &HeartbeatResponse{})
			claimed, err := d.client.ClaimTask(t.Context(), "runtime")
			if err != nil {
				t.Fatal(err)
			}
			d.handleTask(t.Context(), *claimed, 0)
			last := "/api/daemon/tasks/task/" + terminal
			if terminal == "complete" {
				last = "/api/comments/answer/keep-replies"
			}
			if calls[0] != "/api/daemon/runtimes/runtime/tasks/claim" || calls[1] != "/api/daemon/tasks/task/start" || calls[len(calls)-1] != last {
				t.Fatalf("ordinary task lifecycle: %v", calls)
			}
		})
	}
}
