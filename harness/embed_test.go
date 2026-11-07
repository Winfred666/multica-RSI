package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInstallRunsPackagedPythonInstaller(t *testing.T) {
	for _, keepContext := range []bool{false, true} {
		t.Run(fmt.Sprint(keepContext), func(t *testing.T) {
			var systems, libraries int
			var installedSelf, wroteContext bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-Workspace-ID") != "workspace-1" {
					t.Error("installer did not use the native workspace credentials")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.Method + " " + r.URL.Path {
				case "GET /api/skills":
					fmt.Fprint(w, "[]")
				case "POST /api/skills":
					var skill struct {
						Name   string                           `json:"name"`
						System bool                             `json:"is_system_skill"`
						Files  []struct{ Path, Content string } `json:"files"`
					}
					if err := json.NewDecoder(r.Body).Decode(&skill); err != nil {
						t.Error(err)
					}
					if skill.System {
						if libraries != 0 {
							t.Error("system skills arrived after library skills")
						}
						systems++
					} else {
						libraries++
					}
					if skill.Name == "harness-backup-restore" {
						for _, file := range skill.Files {
							if file.Path == "assets/startup-kit.json" {
								var kit map[string]any
								if err := json.Unmarshal([]byte(file.Content), &kit); err != nil {
									t.Error(err)
								}
								installedSelf = kit["skills"] != nil && kit["plugins"] != nil
							}
						}
					}
					json.NewEncoder(w).Encode(map[string]string{"id": skill.Name})
				case "PATCH /api/workspaces/workspace-1":
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					prompt, _ := Files.ReadFile("startup.gotmpl")
					if body["context"] != string(prompt) {
						t.Error("installed prompt differs from packaged prompt")
					}
					wroteContext = true
					fmt.Fprint(w, "{}")
				default:
					t.Errorf("unexpected installer request: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			if err := Install(context.Background(), server.URL, "test-token", "workspace-1", keepContext, false); err != nil {
				t.Fatal(err)
			}
			if systems != 9 || libraries != 5 || !installedSelf || wroteContext == keepContext {
				t.Fatalf("installed system=%d library=%d self=%v context=%v", systems, libraries, installedSelf, wroteContext)
			}
		})
	}
}
