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
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRemoteSOPHeartbeatRunsTargetCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	t.Setenv("MULTICA_PARENT_CONTEXT", "inherited")
	binary := filepath.Join(t.TempDir(), "multica")
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(binary, []byte(`#!/usr/bin/env python3
import os, sys
assert sys.argv[1:] == ['sop', 'run', 'inspect', '--help', 'two words', '', '$(literal)']
assert os.environ['MULTICA_AGENT_ID'] == 'assignee'
assert os.environ['MULTICA_ISSUE_ID'] == 'issue'
assert os.environ['MULTICA_WORKSPACE_ID'] == 'workspace'
assert os.environ['MULTICA_TOKEN'] == 'mat_scoped'
assert os.environ['MULTICA_PARENT_CONTEXT'] == 'inherited'
print(os.getcwd())
print('stderr text', file=sys.stderr)
sys.exit(7)
`), 0700); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	request := protocol.RemoteSOPRequest{ID: "request", IssueID: "issue", AgentID: "assignee", WorkspaceID: "workspace", WorkDir: t.TempDir(),
		Content: `/note !sop-remote inspect {"args":["--help","two words","","$(literal)"]}`, AuthToken: "mat_scoped", CallerType: "agent", CallerID: "caller"}
	results := make(chan TaskMessageData, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/daemon/runtimes/runtime/remote-sops/take":
			_ = json.NewEncoder(w).Encode([]protocol.RemoteSOPRequest{request})
		case "/api/daemon/tasks/request/messages":
			var batch struct {
				Messages []TaskMessageData `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&batch)
			results <- batch.Messages[0]
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("agent API called: %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	d.client, d.cfg.ServerBaseURL = NewClient(server.URL), server.URL
	d.handleHeartbeatActions(t.Context(), "runtime", &HeartbeatResponse{})
	select {
	case result := <-results:
		if result.CallID != "request" || result.Type != "tool_result" || result.Input["exit_code"] != float64(7) || !strings.Contains(result.Output, request.WorkDir) || !strings.Contains(result.Output, "stderr text") {
			t.Fatalf("result=%+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("remote result missing")
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
			err := d.cleanRemoteSOPAnswer(t.Context(), protocol.RemoteSOPRequest{ID: "answer", IssueID: issue, WorkspaceID: "workspace", AuthToken: "mat_scoped", Content: "/note !sop-remote inspect " + string(body)})
			if (err != nil) != scenario.denied || !reflect.DeepEqual(calls, scenario.calls) {
				t.Fatalf("calls=%v err=%v", calls, err)
			}
		})
	}
}
