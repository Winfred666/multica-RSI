package daemon

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestStartupIssueCases(t *testing.T) {
	raw, err := os.ReadFile("../../../harness/testdata/startup-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		IssueID string `json:"issue_id"`
		Example struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"concise_example"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 3 {
		t.Fatalf("got %d recent-run fixtures, want 3", len(cases))
	}
	for _, tc := range cases {
		for _, surface := range []string{"assignment", "comment", "wakeup"} {
			t.Run(tc.Example.Title+"/"+surface, func(t *testing.T) {
				task := Task{IssueID: tc.IssueID, ThreadName: tc.Example.Title,
					IssueDescription: &tc.Example.Description, IssueStatus: "todo"}
				switch surface {
				case "comment":
					task.TriggerCommentID = "comment-1"
				case "wakeup":
					task.WakeupID = "wakeup-1"
				}
				out := BuildPrompt(task, "codex")
				if strings.Contains(out, "multica issue get ") {
					t.Fatalf("startup asks to refetch supplied assignment: %s", out)
				}
				var snapshot struct{ Title, Description, Status string }
				const marker = "Current issue at claim time (JSON task data):\n"
				_, data, found := strings.Cut(out, marker)
				if !found {
					t.Fatal("missing current assignment")
				}
				line, _, _ := strings.Cut(data, "\n")
				if err := json.Unmarshal([]byte(line), &snapshot); err != nil {
					t.Fatal(err)
				}
				if snapshot.Title != tc.Example.Title || snapshot.Description != tc.Example.Description || snapshot.Status != "todo" {
					t.Fatalf("assignment changed in transit: %+v", snapshot)
				}
			})
		}
	}
	// An empty body is supplied context; an omitted field comes from an older server.
	empty := ""
	for _, body := range []*string{nil, &empty} {
		out := BuildPrompt(Task{IssueID: "issue-1", IssueDescription: body}, "codex")
		if strings.Contains(out, "multica issue get ") != (body == nil) {
			t.Fatalf("incorrect read instruction for body %v: %s", body, out)
		}
	}
	body := "A long assignment already in the resumed context."
	out := BuildPrompt(Task{IssueID: "issue-1", TriggerCommentID: "comment-1", PriorSessionID: "session-1",
		IssueDescription: &body, IssueStatus: "in_progress", IssueStateDeltaKnown: true}, "codex")
	if !strings.Contains(out, "The issue is unchanged") || strings.Contains(out, body) {
		t.Fatal("unchanged resumed work should reuse context without repeating the body")
	}
}
