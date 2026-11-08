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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/issues/R-42":
			_, _ = w.Write([]byte(`{"id":"issue","identifier":"R-42","assignee_type":"agent","assignee_id":"agent"}`))
		case "POST /api/issues/issue/comments":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			want := "/note !sop inspect [\"--help\",\"two words\",\"\",\"$(literal)\"]"
			if body["content"] != want {
				t.Errorf("comment=%q want=%q", body["content"], want)
			}
			if len(body) != 1 {
				t.Errorf("unexpected agent routing fields: %v", body)
			}
			_, _ = w.Write([]byte(`{"id":"request"}`))
		case "GET /api/issues/issue/task-runs":
			_, _ = w.Write([]byte(`[{"id":"request","status":"completed"}]`))
		case "GET /api/tasks/request/messages":
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
