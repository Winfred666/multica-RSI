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
assert os.environ['MULTICA_AGENT_ID'] == 'caller'
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
		name, question string
		denied         bool
		calls          []string
	}{
		{"no-reference", "", false, nil},
		{"missing-question", "missing", false, []string{"GET"}},
		{"answered", "question", false, []string{"GET", "DELETE", "PUT"}},
		{"edit-denied", "question", true, []string{"GET", "DELETE", "PUT"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method)
				if r.Header.Get("Authorization") != "Bearer mat_caller" || r.Header.Get("X-Workspace-ID") != "workspace" {
					t.Error("wrong caller context")
				}
				switch r.Method {
				case "GET":
					_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "question", "revision": 3, "content": "/note !sop-render inspect {}\n{\"instruction\":\"Choose\"}"}})
				case "DELETE":
					if r.URL.Path != "/api/comments/answer/keep-replies" {
						t.Error(r.URL.Path)
					}
					w.WriteHeader(http.StatusNoContent)
				case "PUT":
					var update struct {
						Content  string `json:"content"`
						Revision int    `json:"expected_revision"`
					}
					_ = json.NewDecoder(r.Body).Decode(&update)
					if update.Content != "/note !sop-render inspect {}\n{\"args\":[\"approve\",\"two words\"],\"instruction\":\"Choose\"}" || update.Revision != 3 {
						t.Errorf("update=%+v", update)
					}
					if scenario.denied {
						w.WriteHeader(http.StatusForbidden)
					} else {
						_, _ = w.Write([]byte(`{}`))
					}
				}
			}))
			defer server.Close()
			d := &Daemon{cfg: Config{ServerBaseURL: server.URL}}
			body, _ := json.Marshal(remoteSOPCommand{Args: []string{"approve", "two words"}, QuestionCommentID: scenario.question})
			err := d.cleanRemoteSOPAnswer(t.Context(), protocol.RemoteSOPRequest{ID: "answer", IssueID: "issue", WorkspaceID: "workspace", AuthToken: "mat_caller", Content: "/note !sop-remote inspect " + string(body)})
			if (err != nil) != scenario.denied || !reflect.DeepEqual(calls, scenario.calls) {
				t.Fatalf("calls=%v err=%v", calls, err)
			}
		})
	}
}
