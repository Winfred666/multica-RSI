package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestSOPRequestBoundary(t *testing.T) {
	registered := map[string]sopRegistration{"inspect": {Name: "inspect", Inputs: []string{"topic"}}}
	for _, payload := range []string{
		`{"name":"missing","inputs":{"topic":"x"}}`,
		`{"name":"inspect","inputs":{}}`,
		`{"name":"inspect","inputs":{"topic":"x","argv":"--exec"}}`,
		`{"name":"inspect","inputs":{"topic":42}}`,
		`{"name":"inspect","inputs":{"topic":"x"},"executable":"/bin/sh"}`,
		`{"name":"inspect","inputs":{"topic":"x"},"args":["-c","echo unsafe"]}`,
		`{"name":"inspect","inputs":{"topic":"x"}} {}`,
	} {
		if _, _, err := decodeSOPRequest(sopCallPrefix+payload, registered); err == nil {
			t.Errorf("accepted request: %s", payload)
		}
	}
}

func TestSOPCallThreadAndRestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses an executable shell script")
	}
	const issueID = "11111111-1111-4111-8111-111111111111"
	dir := t.TempDir()
	t.Chdir(dir)
	executable := filepath.Join(dir, "inspect")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n[ -z \"$MULTICA_TASK_ID$MULTICA_AGENT_ID$MULTICA_TOKEN\" ] || exit 91\n[ \"$#\" -eq 0 ] || exit 92\n[ \"$TMPDIR\" = \"$PWD\" ] || exit 93\nprintf ran >> counter\ncat\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var roots []sopComment
	var replies []map[string]string
	failReply := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-Workspace-ID") != "ws-1" {
			t.Error("caller credentials or workspace boundary lost")
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/issues/"+issueID:
			_ = json.NewEncoder(w).Encode(map[string]string{"id": issueID, "identifier": "TST-1"})
		case r.Method == "GET" && r.URL.Path == "/api/issues/"+issueID+"/comments":
			_ = json.NewEncoder(w).Encode(roots)
		case r.Method == "POST" && r.URL.Path == "/api/issues/"+issueID+"/comments":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if !strings.HasPrefix(body["content"], "/note ") {
				t.Error("comment could dispatch the agent")
			}
			if body["parent_id"] == "" {
				roots = append(roots, sopComment{ID: "request-1", Content: body["content"]})
			} else {
				if failReply {
					failReply = false
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				replies = append(replies, body)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "receipt"})
		default:
			t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	cmd := &cobra.Command{Use: "call"}
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.Flags().StringArray("input", []string{"topic=$(touch forbidden); --exec /bin/sh"}, "")
	if err := runSOPCall(cmd, []string{issueID, "inspect"}); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(stateDir, "runs"), 0700); err != nil {
		t.Fatal(err)
	}
	registered := map[string]sopRegistration{"inspect": {Name: "inspect", Executable: executable, Inputs: []string{"topic"}}}
	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	// Deliberately inherit an active identity: the standalone process must discard it.
	t.Setenv("MULTICA_TASK_ID", "active-task")
	t.Setenv("MULTICA_AGENT_ID", "active-agent")
	if err := pollSOPCalls(context.Background(), client, issueID, registered, stateDir, time.Second); err == nil {
		t.Fatal("reply failure was hidden")
	}
	for range 2 {
		if err := pollSOPCalls(context.Background(), client, issueID, registered, stateDir, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if len(replies) != 1 || replies[0]["parent_id"] != "request-1" || !strings.Contains(replies[0]["content"], "completed") || !strings.Contains(replies[0]["content"], "$(touch forbidden)") {
		t.Fatalf("wrong native result: %#v", replies)
	}
	count, err := os.ReadFile(filepath.Join(stateDir, "runs", "request-1", "counter"))
	if err != nil || string(count) != "ran" {
		t.Fatalf("execution was not isolated and at most once: %q, %v", count, err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "runs", "request-1", "forbidden")); !os.IsNotExist(err) {
		t.Fatal("input was interpreted as a command")
	}
	roots = append(roots, sopComment{ID: "request-2", Content: sopCallPrefix + `{"name":"unregistered","inputs":{}}`})
	if err := pollSOPCalls(context.Background(), client, issueID, registered, stateDir, time.Second); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 || !strings.Contains(replies[1]["content"], "unregistered SOP") || replies[1]["parent_id"] != "request-2" {
		t.Fatalf("unregistered call was not rejected in its thread: %#v", replies)
	}
}

func TestSOPHostRefusesTaskToken(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("MULTICA_TASK_CONFIG_ROOT", "")
	t.Setenv("MULTICA_TASK_ID", "")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_DAEMON_PORT", "")
	setCLITestServerEnv(t, "http://localhost:1")
	t.Setenv("MULTICA_TOKEN", "mat_active-task")
	if _, err := sopLocalClient(&cobra.Command{Use: "serve"}); err == nil || !strings.Contains(err.Error(), "member profile") {
		t.Fatalf("task token accepted by SOP host: %v", err)
	}
}
