package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRemoteSOPHeartbeatRunsOnlyPython(t *testing.T) {
	for _, argv := range []string{`["--help","two words","","$(literal)","--large"]`, `["--fail"]`, `[]`, `bad-json`} {
		t.Run(argv, func(t *testing.T) {
			skills, _ := json.Marshal([]SkillData{{Name: "inspect", Content: "---\nname: inspect\n---", Files: []SkillFileData{{Path: "wrapper.py", Content: "import json,os,sys\nprint(json.dumps({'args':sys.argv[1:],'task_id':os.environ['MULTICA_TASK_ID'],'token':os.environ['MULTICA_TOKEN']}))\nprint('stderr text',file=sys.stderr)\nif '--fail' in sys.argv: sys.exit(7)\nif '--large' in sys.argv: print('x'*70000)\n"}}}})
			name := "inspect"
			if argv == `[]` {
				name = "missing"
			}
			request := protocol.RemoteSOPRequest{ID: "request", IssueID: "issue", AgentID: "assignee", WorkspaceID: "workspace", Content: "/note !sop " + name + " " + argv, Skills: skills, AuthToken: "mat_scoped"}
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
			// No configured agent runtimes, providers, sessions, or execution environments.
			d := &Daemon{client: NewClient(server.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil)), cfg: Config{ServerBaseURL: server.URL, WorkspacesRoot: t.TempDir()}}
			d.client.SetToken("host-owner-token")
			d.handleHeartbeatActions(t.Context(), "runtime", &HeartbeatResponse{})
			select {
			case result := <-results:
				if result.CallID != "request" || result.Type != "tool_result" {
					t.Fatalf("log=%+v", result)
				}
				if argv == "bad-json" {
					if result.Content == "" {
						t.Fatal("missing parse error")
					}
					return
				}
				if name == "missing" {
					if !strings.Contains(result.Output, "inspect") {
						t.Fatal("missing available SOP list")
					}
					return
				}
				if !strings.Contains(result.Output, `"task_id": "0"`) || !strings.Contains(result.Output, "mat_scoped") || !strings.Contains(result.Output, "stderr text") {
					t.Fatalf("output=%q", result.Output)
				}
				if strings.Contains(argv, "--fail") && result.Input["exit_code"] != float64(7) {
					t.Fatalf("exit=%v", result.Input)
				}
				if strings.Contains(argv, "--large") && (len(result.Output) < 70000 || !strings.Contains(result.Output, `"two words", "", "$(literal)"`)) {
					t.Fatal("arguments or output changed")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("remote result missing")
			}
		})
	}
}
