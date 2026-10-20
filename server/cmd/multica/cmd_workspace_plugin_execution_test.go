package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestWorkspacePluginExecutionCLI(t *testing.T) {
	for _, action := range []string{"status", "enable", "disable"} {
		t.Run(action, func(t *testing.T) {
			mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				want := map[string]string{"status": "GET", "enable": "PUT", "disable": "DELETE"}[action]
				if r.Method != want || r.URL.Path != "/api/workspaces/"+testWorkspaceMcpID+"/plugins/execution" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if action == "enable" {
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["token"] != "mul_supplied_by_alice" {
						t.Error("supplied PAT missing")
					}
				}
				if action == "disable" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				_, _ = w.Write([]byte(`{"configured":true,"enabled":true,"run_as":{"id":"alice","name":"Alice"},"token":"must-not-print"}`))
			})
			cmd := newWorkspacePluginExecutionCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetIn(strings.NewReader("mul_supplied_by_alice\n"))
			args := []string{action}
			if action == "enable" {
				args = append(args, "--token-stdin")
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "must-not-print") || strings.Contains(out.String(), "mul_") {
				t.Fatal("credential leaked to stdout")
			}
			if action != "disable" && !strings.Contains(out.String(), "Alice") {
				t.Fatal("owner missing")
			}
		})
	}
}

func TestWorkspacePluginExecutionRequiresExplicitInput(t *testing.T) {
	cmd := newWorkspacePluginExecutionCmd()
	cmd.SetArgs([]string{"enable"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--token-stdin") {
		t.Fatal("accepted implicit credential", err)
	}
}
