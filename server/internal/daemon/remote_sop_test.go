package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
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
assert os.environ['MULTICA_TOKEN'] == 'mat_scoped'
print(os.getcwd())
print('stderr text', file=sys.stderr)
sys.exit(7 if os.environ['FIXTURE_TERMINAL'] == 'fail' else 0)
`))
			task := Task{ID: "task", AgentID: "assignee", RuntimeID: "runtime", IssueID: "issue", WorkspaceID: "workspace", PriorWorkDir: t.TempDir(), AuthToken: "mat_scoped", Agent: &AgentData{ID: "assignee"},
				TriggerCommentID: "answer", TriggerCommentContent: `!sop-remote inspect {"args":["--help","two words","","$(literal)"]}`}
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.URL.Path)
				switch r.URL.Path {
				case "/api/daemon/runtimes/runtime/tasks/claim":
					_ = json.NewEncoder(w).Encode(map[string]any{"task": task})
				case "/api/daemon/tasks/task/messages":
					var batch struct {
						Messages []TaskMessageData `json:"messages"`
					}
					_ = json.NewDecoder(r.Body).Decode(&batch)
					if len(batch.Messages) != 2 || batch.Messages[0].Seq != 1 || batch.Messages[1].CallID != "answer" || !strings.Contains(batch.Messages[1].Output, task.PriorWorkDir) || !strings.Contains(batch.Messages[1].Output, "stderr text") {
						t.Errorf("messages=%+v", batch.Messages)
					}
				case "/api/daemon/tasks/task/start", "/api/daemon/tasks/task/progress", "/api/daemon/tasks/task/" + terminal, "/api/daemon/tasks/task/status":
				default:
					t.Errorf("unexpected API (poll or model): %s", r.URL.Path)
				}
				_, _ = w.Write([]byte(`{"status":"running"}`))
			}))
			defer server.Close()
			d := &Daemon{client: NewClient(server.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil)), activeEnvRoots: map[string]int{}, runtimeIndex: map[string]Runtime{"runtime": {Provider: "no-model"}}, cfg: Config{ServerBaseURL: server.URL, WorkspacesRoot: t.TempDir()}}
			d.runner = taskRunnerFunc(d.runTask)
			d.handleHeartbeatActions(t.Context(), "runtime", &HeartbeatResponse{})
			claimed, err := d.client.ClaimTask(t.Context(), "runtime")
			if err != nil {
				t.Fatal(err)
			}
			d.handleTask(t.Context(), *claimed, 0)
			if calls[0] != "/api/daemon/runtimes/runtime/tasks/claim" || calls[1] != "/api/daemon/tasks/task/start" || calls[len(calls)-1] != "/api/daemon/tasks/task/"+terminal {
				t.Fatalf("ordinary task lifecycle: %v", calls)
			}
		})
	}
}

func TestRemoteSOPQuestionnaireCleanup(t *testing.T) {
	for _, scenario := range []struct {
		name, question  string
		forward, denied bool
		calls           []string
	}{
		{"no-reference", "", false, false, nil},
		{"missing-question", "missing", false, false, []string{"GET"}},
		{"answered", "question", false, false, []string{"GET", "DELETE", "PUT"}},
		{"edit-denied", "question", false, true, []string{"GET", "DELETE", "PUT"}},
		{"forward-source", "question", true, false, []string{"GET", "DELETE", "PUT", "GET", "PUT"}},
		{"forward-recipient", "question", true, false, []string{"GET", "DELETE", "PUT", "GET", "PUT"}},
		{"forward-denied", "question", true, true, []string{"GET", "DELETE", "PUT", "GET", "PUT"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var calls []string
			args := []string{"approve", "two words"}
			header := "/note !sop-render inspect {}"
			rendered := map[string]any{"instruction": "Choose", "args": args}
			if scenario.forward {
				args = append(args, "--issue", "issue")
				header = `/note !sop-render inspect {"issue_id":"recipient"}`
				rendered["forwarded_comment_id"], rendered["args"] = "forward", args
			}
			accepted, _ := json.Marshal(rendered)
			delete(rendered, "args")
			pending, _ := json.Marshal(rendered)
			link := "https://multica.example/workspace/issues/issue#comment-question"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method)
				if r.Header.Get("Authorization") != "Bearer mat_scoped" || r.Header.Get("X-Workspace-ID") != "workspace" {
					t.Error("wrong target context")
				}
				switch r.Method {
				case "GET":
					id, content := "question", header+"\n"+string(pending)
					if r.URL.Path == "/api/issues/recipient/comments" {
						if r.URL.Query().Get("thread") != "forward" {
							t.Error(r.URL.String())
						}
						id, content = "forward", "Owner prose\n\n[Question]("+link+")"
					} else if r.URL.Path != "/api/issues/issue/comments" {
						t.Error(r.URL.String())
					}
					_ = json.NewEncoder(w).Encode([]map[string]any{{"id": id, "revision": 3, "content": content}})
				case "DELETE":
					if r.URL.Path != "/api/comments/answer/keep-replies" {
						t.Error(r.URL.Path)
					}
					w.WriteHeader(http.StatusNoContent)
				case "PUT":
					var update map[string]json.RawMessage
					_ = json.NewDecoder(r.Body).Decode(&update)
					if len(update) != 2 {
						t.Error("only content and revision may change")
					}
					var content string
					var revision int
					_ = json.Unmarshal(update["content"], &content)
					_ = json.Unmarshal(update["expected_revision"], &revision)
					expected := header + "\n" + string(accepted)
					forward := r.URL.Path == "/api/comments/forward"
					if !forward && r.URL.Path != "/api/comments/question" {
						t.Error(r.URL.Path)
					}
					if forward {
						expected = "Owner prose\n\n" + link + "\n\n" + expected
					}
					if content != expected || revision != 3 {
						t.Errorf("content=%s revision=%d", content, revision)
					}
					if scenario.denied && (forward || !scenario.forward) {
						w.WriteHeader(http.StatusForbidden)
					} else {
						_, _ = w.Write([]byte(`{}`))
					}
				default:
					t.Error(r.Method)
				}
			}))
			defer server.Close()
			d := &Daemon{cfg: Config{ServerBaseURL: server.URL}}
			issue := "issue"
			if scenario.name == "forward-recipient" {
				issue = "recipient"
			}
			body, _ := json.Marshal(remoteSOPCommand{Args: args, QuestionCommentID: scenario.question})
			err := d.cleanRemoteSOPAnswer(t.Context(), Task{TriggerCommentID: "answer", IssueID: issue, WorkspaceID: "workspace", AuthToken: "mat_scoped", TriggerCommentContent: "!sop-remote inspect " + string(body)})
			if (err != nil) != scenario.denied || !reflect.DeepEqual(calls, scenario.calls) {
				t.Fatalf("calls=%v err=%v", calls, err)
			}
		})
	}
}
