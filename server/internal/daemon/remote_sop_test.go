package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func remoteSOPTestSkill() SkillData {
	return SkillData{Name: "inspect", Content: "---\nname: inspect\ndescription: Test SOP\n---\n", Files: []SkillFileData{
		{Path: "DEBUG.md", Content: "Fixture SOP"},
		{Path: "wrapper.py", Content: "import json, os, sys\nprint(json.dumps({'args':sys.argv[1:],'task_id':os.environ['MULTICA_TASK_ID']}))\nprint('stderr text',file=sys.stderr)\nif '--fail' in sys.argv: sys.exit(7)\nif '--large' in sys.argv: print('x'*70000)\n"},
	}}
}

func TestRemoteSOPBypassesProvider(t *testing.T) {
	for _, args := range []string{`["--help","two words","","$(touch forbidden)","--large"]`, `["--fail"]`, `[]`, `not-json`} {
		t.Run(args, func(t *testing.T) {
			d, marker, cleanup := newLeaderReuseTestDaemon(t)
			defer cleanup()
			task := leaderReuseTestTask("remote-task")
			task.Agent.Skills = []SkillData{remoteSOPTestSkill()}
			task.TriggerCommentID, task.TriggerCommentContent = "request", "!sop inspect "+args
			var messages []TaskMessageData
			consumed := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/consume"):
					if len(messages) != 1 {
						t.Error("request was not transferred before deletion")
					}
					consumed = true
				case strings.HasSuffix(r.URL.Path, "/messages"):
					var batch struct {
						Messages []TaskMessageData `json:"messages"`
					}
					_ = json.NewDecoder(r.Body).Decode(&batch)
					messages = append(messages, batch.Messages...)
				case strings.HasSuffix(r.URL.Path, "/comments"):
					t.Error("SOP posted a comment")
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			d.client, d.cfg.ServerBaseURL = NewClient(srv.URL), srv.URL
			result, err := d.runTask(t.Context(), task, "claude", 0, d.logger)
			if err != nil || result.Status != "completed" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("provider started")
			}
			if !consumed || len(messages) != 2 || messages[1].Type != "tool_result" {
				t.Fatalf("execution log=%+v", messages)
			}
			out := messages[1]
			if args == "not-json" {
				if out.Content == "" {
					t.Fatal("missing parser failure")
				}
				return
			}
			if !strings.Contains(out.Output, `"task_id": "0"`) || !strings.Contains(out.Output, "stderr text") {
				t.Fatalf("output=%q", out.Output)
			}
			if strings.Contains(args, "--large") && len(out.Output) < 70000 {
				t.Fatal("output truncated")
			}
			if strings.Contains(args, "--fail") && out.Input["exit_code"] != float64(7) {
				t.Fatalf("exit code=%v", out.Input)
			}
			if strings.Contains(args, "two words") && !strings.Contains(out.Output, `"two words", "", "$(touch forbidden)"`) {
				t.Fatal("arguments changed")
			}
		})
	}
}

func TestRemoteSOPSteeringStaysOutOfModel(t *testing.T) {
	var claims, injections atomic.Int32
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := taskSupplementTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/claim"):
			if claims.Add(1) == 1 {
				_, _ = w.Write([]byte(`{"comment_id":"request","content":"!sop missing []"}`))
			} else {
				_, _ = w.Write([]byte(`{}`))
			}
		case strings.HasSuffix(r.URL.Path, "/messages"):
			var batch struct {
				Messages []TaskMessageData `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&batch)
			if batch.Messages[0].Type == "tool_result" {
				if !strings.Contains(batch.Messages[0].Output, "inspect") {
					t.Error("missing available SOP list")
				}
				cancel()
			}
			_, _ = w.Write([]byte(`{}`))
		case strings.HasSuffix(r.URL.Path, "/consume"):
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	})
	workDir := t.TempDir()
	skill := filepath.Join(workDir, ".agents", "skills", "inspect")
	if err := os.MkdirAll(skill, 0700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"DEBUG.md", "wrapper.py"} {
		if err := os.WriteFile(filepath.Join(skill, file), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var seq atomic.Int32
	ctx = context.WithValue(ctx, remoteSOPContextKey{}, remoteSOPContext{task: leaderReuseTestTask("steer"), env: &execenv.Environment{WorkDir: workDir}, provider: "claude", seq: &seq})
	session := &agent.Session{SupplementReady: func() bool { return true }, Supplement: func(context.Context, string) error { injections.Add(1); return nil }}
	d.runTaskSupplementLoop(ctx, session, "steer", make(chan struct{}), d.logger)
	if injections.Load() != 0 {
		t.Fatal("remote request injected into model")
	}
}

func TestRemoteSOPPreservesOrdinaryCoalescedWork(t *testing.T) {
	d, _, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()
	task := leaderReuseTestTask("mixed")
	task.TriggerCommentID, task.TriggerCommentContent = "remote", "!sop missing []"
	task.CoalescedCommentIDs = []string{"ordinary"}
	task.CoalescedComments = []CoalescedCommentData{{ID: "ordinary", Content: "Keep working"}}
	var seq atomic.Int32
	state := remoteSOPContext{task: task, env: &execenv.Environment{WorkDir: t.TempDir()}, seq: &seq}
	handled, err := d.interceptTaskRemoteSOPs(t.Context(), &task, state)
	if err != nil || handled {
		t.Fatalf("ordinary work swallowed: %v %v", handled, err)
	}
	if task.TriggerCommentID != "ordinary" || task.TriggerCommentContent != "Keep working" || len(task.CoalescedComments) != 0 {
		t.Fatalf("task context=%+v", task)
	}
}
