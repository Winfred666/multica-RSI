package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspacePluginSettingsCLI(t *testing.T) {
	for _, action := range []string{"list", "configure"} {
		t.Run(action, func(t *testing.T) {
			mcpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				path, method := "/api/workspaces/"+testWorkspaceMcpID+"/plugins", "GET"
				if action == "configure" {
					path += "/installed-plugin/config"
					method = "PUT"
					var body struct{ Values map[string]string }
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Values["prompt"] != "Review\nthen continue" {
						t.Error("prompt was not preserved", err)
					}
				}
				if r.Method != method || r.URL.Path != path {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				_, _ = w.Write([]byte(`{"plugins":[]}`))
			})
			cmd := newWorkspacePluginCmd()
			cmd.SetOut(&bytes.Buffer{})
			args := []string{action}
			if action == "configure" {
				file := filepath.Join(t.TempDir(), "settings.json")
				if err := os.WriteFile(file, []byte(`{"prompt":"Review\nthen continue"}`), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "installed-plugin", "--config-file", file)
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
