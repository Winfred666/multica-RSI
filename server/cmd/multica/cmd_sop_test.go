package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func TestSOPRegisterAdvertisesInputs(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, key := range []string{"MULTICA_TASK_CONFIG_ROOT", "MULTICA_TASK_ID", "MULTICA_AGENT_ID", "MULTICA_DAEMON_PORT"} {
		t.Setenv(key, "")
	}
	const issueID = "11111111-1111-4111-8111-111111111111"
	var advertised string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/issues/"+issueID:
			_ = json.NewEncoder(w).Encode(map[string]string{"id": issueID, "identifier": "TST-1"})
		case r.Method == "PUT" && r.URL.Path == "/api/issues/"+issueID+"/metadata/sop.inspect":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			advertised = body["value"]
			_, _ = w.Write([]byte(`{"metadata":{}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	profile := "sop-test-" + filepath.Base(t.TempDir())
	dir, err := cli.SOPStateDir(profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(dir)) })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{Use: "register"}
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.Flags().String("profile", profile, "")
	cmd.Flags().String("executable", executable, "")
	cmd.Flags().StringArray("arg", []string{"/opt/sops/inspect.py"}, "")
	cmd.Flags().StringSlice("input", []string{"topic"}, "")
	if err := runSOPRegister(cmd, []string{issueID, "inspect"}); err != nil {
		t.Fatal(err)
	}
	entries, err := cli.ReadSOPRegistry(dir)
	if err != nil || len(entries) != 1 || entries[0].Args[0] != "/opt/sops/inspect.py" {
		t.Fatalf("registry: %+v, %v", entries, err)
	}
	if advertised != `{"inputs":["topic"]}` {
		t.Fatalf("metadata: %q", advertised)
	}
	for _, sub := range sopCmd.Commands() {
		if sub.Name() != "register" {
			t.Fatalf("unexpected invocation command: %s", sub.Name())
		}
	}
}

func TestSOPHostRefusesTaskToken(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, key := range []string{"MULTICA_TASK_CONFIG_ROOT", "MULTICA_TASK_ID", "MULTICA_AGENT_ID", "MULTICA_DAEMON_PORT"} {
		t.Setenv(key, "")
	}
	setCLITestServerEnv(t, "http://localhost:1")
	t.Setenv("MULTICA_TOKEN", "mat_active-task")
	if _, err := sopLocalClient(&cobra.Command{Use: "register"}); err == nil || !strings.Contains(err.Error(), "member profile") {
		t.Fatalf("task token accepted by SOP host: %v", err)
	}
}
