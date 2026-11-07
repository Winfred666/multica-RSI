package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/spf13/cobra"
)

func TestRemoteSOPCommandPassesArgumentsAndReadsLog(t *testing.T) {
	t.Chdir(t.TempDir())
	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/issues/R-42":
			_, _ = w.Write([]byte(`{"id":"issue","identifier":"R-42","assignee_type":"agent","assignee_id":"agent"}`))
		case "POST /api/issues/issue/comments":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			want := "!sop inspect [\"--help\",\"two words\",\"\",\"$(literal)\"]\n\n[@assignee](mention://agent/agent)"
			if body["content"] != want {
				t.Errorf("comment=%q want=%q", body["content"], want)
			}
			steer, _ := body["steer_task_ids"].([]any)
			if len(steer) != 1 || steer[0] != "run" {
				t.Fatalf("active assignee was not steered: %v", steer)
			}
			posted = true
			_, _ = w.Write([]byte(`{"id":"request"}`))
		case "GET /api/issues/issue/task-runs":
			if posted {
				_, _ = w.Write([]byte(`[{"id":"run","agent_id":"agent","status":"completed"}]`))
			} else {
				_, _ = w.Write([]byte(`[{"id":"run","agent_id":"agent","status":"running","can_supplement":true}]`))
			}
		case "GET /api/tasks/run/messages":
			_ = json.NewEncoder(w).Encode([]protocol.TaskMessagePayload{
				{Seq: 1, Type: "tool_result", Tool: "remote-sop", CallID: "different-request", Output: "wrong result"},
				{Seq: 2, Type: "tool_result", Tool: "remote-sop", CallID: "request", Output: "faithful output\n", Input: map[string]any{"exit_code": 0}},
			})
		default:
			t.Errorf("unexpected API call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	for _, key := range []string{"MULTICA_TASK_CONFIG_ROOT", "MULTICA_TASK_ID", "MULTICA_AGENT_ID", "MULTICA_DAEMON_PORT"} {
		t.Setenv(key, "")
	}
	cmd := &cobra.Command{Use: remoteSOPCmd.Use, RunE: remoteSOPCmd.RunE}
	cmd.Flags().SetInterspersed(false)
	root := &cobra.Command{Use: "multica"}
	root.PersistentFlags().String("profile", "", "")
	root.AddCommand(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	root.SetArgs([]string{"--profile", "r224-fixture", "remote-sop", "R-42", "inspect", "--help", "two words", "", "$(literal)"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[R-42]\nfaithful output\n" {
		t.Fatalf("output=%q", out.String())
	}
	if strings.Contains(out.String(), "wrong result") {
		t.Fatal("returned another call's output")
	}
}
