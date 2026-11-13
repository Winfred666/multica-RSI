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
