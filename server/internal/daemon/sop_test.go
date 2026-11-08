package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func registerTestSOP(t *testing.T, d *Daemon, task Task) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses /bin/sh")
	}
	d.cfg.Profile = "sop-test-" + filepath.Base(t.TempDir())
	dir, err := cli.SOPStateDir(d.cfg.Profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(dir)) })
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	entry := cli.SOPRegistration{ServerURL: d.cfg.ServerBaseURL, WorkspaceID: task.WorkspaceID, IssueID: task.IssueID, Name: "inspect", Executable: "/bin/sh", Args: []string{"-c", `printf ran >> counter; cat; [ "$MULTICA_TOKEN" = "` + task.AuthToken + `" ]`}, Inputs: []string{"decision"}}
	if err := cli.WriteSOPJSON(filepath.Join(dir, "registry.json"), []cli.SOPRegistration{entry}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSOPTaskBypassesProviderAndFallsBack(t *testing.T) {
	d, argsFile, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()
	task := leaderReuseTestTask("sop-task")
	task.TriggerCommentID = "request-1"
	task.TriggerCommentContent = `/sop {"name":"inspect","inputs":{"decision":{"next":"$(touch forbidden)","ok":true}}}`
	var replies []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/comments") {
			if r.Header.Get("Authorization") != "Bearer "+task.AuthToken || r.Header.Get("X-Workspace-ID") != task.WorkspaceID {
				t.Error("task credential or workspace lost")
			}
			var reply map[string]string
			_ = json.NewDecoder(r.Body).Decode(&reply)
			replies = append(replies, reply)
		}
		_, _ = w.Write([]byte(`{"id":"reply"}`))
	}))
	defer srv.Close()
	d.client = NewClient(srv.URL)
	d.cfg.ServerBaseURL = srv.URL
	dir := registerTestSOP(t, d, task)
	result, err := d.runTask(t.Context(), task, "claude", 0, d.logger)
	if err != nil || result.Status != "completed" {
		t.Fatalf("task: %+v %v", result, err)
	}
	if _, err := os.Stat(argsFile); !os.IsNotExist(err) {
		t.Fatalf("provider started: %v", err)
	}
	if len(replies) != 1 || replies[0]["parent_id"] != task.TriggerCommentID || !strings.HasPrefix(replies[0]["content"], "/note SOP inspect completed") || !strings.Contains(replies[0]["content"], `"ok":true`) {
		t.Fatalf("reply: %+v", replies)
	}
	// A redelivered comment must reuse its saved result.
	again := task
	if !d.interceptTaskSOPs(t.Context(), &again) || len(replies) != 1 {
		t.Fatal("successful call replayed")
	}
	count, err := os.ReadFile(filepath.Join(dir, "runs", task.TriggerCommentID, "counter"))
	if err != nil || string(count) != "ran" {
		t.Fatalf("execution count: %s %v", count, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "runs", task.TriggerCommentID, "forbidden")); !os.IsNotExist(err) {
		t.Fatal("interpreted JSON as shell")
	}
	// An unregistered request reaches the normal provider.
	task.ID = "sop-fallback"
	task.TriggerCommentID = "request-missing"
	task.TriggerCommentContent = `/sop {"name":"missing","inputs":{}}`
	result, err = d.runTask(t.Context(), task, "claude", 0, d.logger)
	if err != nil || result.Status != "completed" {
		t.Fatalf("fallback task: %+v %v", result, err)
	}
	if _, err := os.Stat(argsFile); err != nil {
		t.Fatal("failed call did not launch provider", err)
	}
	// An ordinary coalesced comment still needs the model even when the latest SOP succeeds.
	task.TriggerCommentID = "request-mixed"
	task.TriggerCommentContent = `/sop {"name":"inspect","inputs":{"decision":{}}}`
	task.CoalescedCommentIDs = []string{"ordinary"}
	task.CoalescedComments = []CoalescedCommentData{{ID: "ordinary", Content: "Review the result"}}
	if d.interceptTaskSOPs(t.Context(), &task) {
		t.Fatal("ordinary coalesced work swallowed")
	}
}

func TestSOPSteeringBypassAndFailure(t *testing.T) {
	for _, fail := range []string{"", "execution", "delivery"} {
		t.Run("failure="+fail, func(t *testing.T) {
			var claims, injections atomic.Int32
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			d := taskSupplementTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/claim"):
					if claims.Add(1) == 1 {
						_, _ = w.Write([]byte(`{"comment_id":"request-steer","content":"/sop {\"name\":\"inspect\",\"inputs\":{\"decision\":{}}}"}`))
					} else {
						_, _ = w.Write([]byte(`{}`))
					}
				case strings.HasSuffix(r.URL.Path, "/comments"):
					if fail == "delivery" {
						w.WriteHeader(503)
					} else {
						_, _ = w.Write([]byte(`{"id":"reply"}`))
					}
				case strings.HasSuffix(r.URL.Path, "/ack"):
					var ack map[string]any
					_ = json.NewDecoder(r.Body).Decode(&ack)
					if ack["delivered"] != true {
						t.Error("request not acknowledged")
					}
					cancel()
					_, _ = w.Write([]byte(`{}`))
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(404)
				}
			})
			d.cfg.ServerBaseURL = d.client.baseURL
			task := leaderReuseTestTask("steering-task")
			dir := registerTestSOP(t, d, task)
			if fail == "execution" {
				entries, _ := cli.ReadSOPRegistry(dir)
				entries[0].Args = []string{"-c", "echo failed; exit 7"}
				if err := cli.WriteSOPJSON(filepath.Join(dir, "registry.json"), entries); err != nil {
					t.Fatal(err)
				}
			}
			session := &agent.Session{SupplementReady: func() bool { return true }, Supplement: func(_ context.Context, content string) error {
				injections.Add(1)
				if !strings.Contains(content, "Daemon SOP call failed") {
					t.Error("fallback diagnostic missing")
				}
				return nil
			}}
			ctx = context.WithValue(ctx, sopTaskKey{}, task)
			d.runTaskSupplementLoop(ctx, session, task.ID, make(chan struct{}), d.logger)
			want := int32(0)
			if fail != "" {
				want = 1
			}
			if injections.Load() != want {
				t.Fatalf("provider injections=%d want %d", injections.Load(), want)
			}
			if fail == "delivery" {
				// Delivery can be retried without repeating script side effects.
				count, err := os.ReadFile(filepath.Join(dir, "runs", "request-steer", "counter"))
				if err != nil || string(count) != "ran" {
					t.Fatalf("saved execution: %s %v", count, err)
				}
			}
		})
	}
}

func TestSOPRequestBoundary(t *testing.T) {
	task := leaderReuseTestTask("task")
	entries := []cli.SOPRegistration{{ServerURL: "http://host", WorkspaceID: task.WorkspaceID, IssueID: task.IssueID, Name: "inspect", Inputs: []string{"decision"}}}
	for _, content := range []string{
		`{"name":"missing","inputs":{"decision":{}}}`,
		`{"name":"inspect","inputs":{}}`,
		`{"name":"inspect","inputs":{"decision":{}},"executable":"/bin/sh"}`,
		`{"name":"inspect","inputs":{"decision":{}}} {}`,
	} {
		if _, _, err := decodeSOPRequest(sopCallPrefix+content, entries, task, "http://host"); err == nil {
			t.Fatalf("accepted %s", content)
		}
	}
	valid := sopCallPrefix + `{"name":"inspect","inputs":{"decision":{}}}` + "\n\n[@Worker](mention://agent/11111111-1111-4111-8111-111111111111)"
	if _, _, err := decodeSOPRequest(valid, entries, task, "http://host"); err != nil {
		t.Fatal(err)
	}
	task.WorkspaceID = "other"
	if _, _, err := decodeSOPRequest(valid, entries, task, "http://host"); err == nil {
		t.Fatal("cross-workspace registration executed")
	}
}
