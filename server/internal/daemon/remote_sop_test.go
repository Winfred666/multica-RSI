package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRemoteSOPQuestionnaireCleanup(t *testing.T) {
	for _, scenario := range []struct {
		name, question  string
		forward, denied bool
		calls           []string
	}{
		{"no-reference", "", false, false, nil},
		{"missing-question", "missing", false, false, []string{"GET"}},
		{"answered", "question", false, false, []string{"GET", "DELETE", "PUT"}},
		{"edit-denied", "question", false, true, []string{"GET", "DELETE", "PUT"}},
		{"forward-source", "question", true, false, []string{"GET", "DELETE", "PUT", "GET", "PUT"}},
		{"forward-recipient", "question", true, false, []string{"GET", "DELETE", "PUT", "GET", "PUT"}},
		{"forward-denied", "question", true, true, []string{"GET", "DELETE", "PUT", "GET", "PUT"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var calls []string
			args := []string{"approve", "two words"}
			header := "/note !sop-render inspect {}"
			rendered := map[string]any{"instruction": "Choose", "args": args}
			if scenario.forward {
				args = append(args, "--issue", "issue")
				header = `/note !sop-render inspect {"issue_id":"recipient"}`
				rendered["forwarded_comment_id"], rendered["args"] = "forward", args
			}
			accepted, _ := json.Marshal(rendered)
			delete(rendered, "args")
			pending, _ := json.Marshal(rendered)
			link := "https://multica.example/workspace/issues/issue#comment-question"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method)
				if r.Header.Get("Authorization") != "Bearer pat_local" || r.Header.Get("X-Workspace-ID") != "workspace" {
					t.Error("wrong target context")
				}
				switch r.Method {
				case "GET":
					id, content := "question", header+"\n"+string(pending)
					if r.URL.Path == "/api/issues/recipient/comments" {
						if r.URL.Query().Get("thread") != "forward" {
							t.Error(r.URL.String())
						}
						id, content = "forward", "Owner prose\n\n[Question]("+link+")"
					} else if r.URL.Path != "/api/issues/issue/comments" {
						t.Error(r.URL.String())
					}
					_ = json.NewEncoder(w).Encode([]map[string]any{{"id": id, "revision": 3, "content": content}})
				case "DELETE":
					if r.URL.Path != "/api/comments/answer/keep-replies" {
						t.Error(r.URL.Path)
					}
					w.WriteHeader(http.StatusNoContent)
				case "PUT":
					var update map[string]json.RawMessage
					_ = json.NewDecoder(r.Body).Decode(&update)
					if len(update) != 2 {
						t.Error("only content and revision may change")
					}
					var content string
					var revision int
					_ = json.Unmarshal(update["content"], &content)
					_ = json.Unmarshal(update["expected_revision"], &revision)
					expected := header + "\n" + string(accepted)
					forward := r.URL.Path == "/api/comments/forward"
					if !forward && r.URL.Path != "/api/comments/question" {
						t.Error(r.URL.Path)
					}
					if forward {
						expected = "Owner prose\n\n" + link + "\n\n" + expected
					}
					if content != expected || revision != 3 {
						t.Errorf("content=%s revision=%d", content, revision)
					}
					if scenario.denied && (forward || !scenario.forward) {
						w.WriteHeader(http.StatusForbidden)
					} else {
						_, _ = w.Write([]byte(`{}`))
					}
				default:
					t.Error(r.Method)
				}
			}))
			defer server.Close()
			d := &Daemon{client: NewClient(server.URL), cfg: Config{ServerBaseURL: server.URL}}
			d.client.SetToken("pat_local")
			issue := "issue"
			if scenario.name == "forward-recipient" {
				issue = "recipient"
			}
			body, _ := json.Marshal(remoteSOPCommand{Args: args, QuestionCommentID: scenario.question})
			err := d.cleanRemoteSOPAnswer(t.Context(), protocol.RemoteSOPRequest{ID: "answer", IssueID: issue, WorkspaceID: "workspace", Content: "!sop-remote inspect " + string(body)})
			if (err != nil) != scenario.denied || !reflect.DeepEqual(calls, scenario.calls) {
				t.Fatalf("calls=%v err=%v", calls, err)
			}
		})
	}
}
