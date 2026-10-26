package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestIssueBranchPreparationUsesCreatedEvents(t *testing.T) {
	frames := make(chan protocol.Message)
	scanned := make(chan struct{}, 2)
	prepared := make(chan string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws":
			if r.URL.Query().Get("workspace_id") != "workspace" {
				t.Error("missing workspace subscription")
			}
			conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			var auth struct {
				Type    string
				Payload struct{ Token string }
			}
			if err := conn.ReadJSON(&auth); err != nil || auth.Type != "auth" || auth.Payload.Token != "daemon-token" {
				t.Errorf("invalid event stream authentication: %+v, %v", auth, err)
				return
			}
			_ = conn.WriteJSON(protocol.Message{Type: "auth_ack"})
			for frame := range frames {
				if err := conn.WriteJSON(frame); err != nil {
					return
				}
			}
		case "/api/issues":
			_, _ = w.Write([]byte(`{"issues":[],"total":0}`))
			scanned <- struct{}{}
		case "/api/issues/created":
			_, _ = w.Write([]byte(`{"id":"created","metadata":{"git_branch":"issue/created"}}`))
			prepared <- "created"
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	d := &Daemon{client: NewClient(srv.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	d.client.SetToken("daemon-token")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- d.watchIssueBranchConnection(ctx, "workspace") }()
	select {
	case <-scanned:
	case <-time.After(3 * time.Second):
		t.Fatal("no connection recovery scan")
	}
	frames <- protocol.Message{Type: protocol.EventIssueUpdated, Payload: json.RawMessage(`{"issue":{"id":"unrelated"}}`)}
	frames <- protocol.Message{Type: protocol.EventIssueCreated, Payload: json.RawMessage(`{"issue":{"id":"created"}}`)}
	select {
	case <-prepared:
	case <-time.After(3 * time.Second):
		t.Fatal("issue-created event did not prepare its branch")
	}
	close(frames)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("event stream did not stop on disconnect")
	}
	if len(scanned) != 0 || len(prepared) != 0 {
		t.Fatal("live events caused a workspace scan or unrelated issue fetch")
	}
}

func TestPrepareWorkspaceIssueBranchesWithoutTasks(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-b", "main")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial")
	rows := []*worktreeIssue{
		{ID: "child", Identifier: "MUL-2", ProjectID: "project", ParentIssueID: "root", Status: "backlog"},
		{ID: "root", Identifier: "MUL-1", ProjectID: "project", Status: "backlog"},
		{ID: "terminal", ProjectID: "project", Status: "wont_do", StatusCategory: "closed"},
		{ID: "in-place", ProjectID: "in-place", Status: "backlog"},
		{ID: "other-daemon", ProjectID: "other-daemon", Status: "backlog"},
	}
	var mu sync.Mutex
	rejectPublication := true
	writes, pages := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer daemon-user-token" || r.Header.Get("X-Workspace-ID") != "workspace" {
			t.Error("background preparation must use the daemon's workspace-scoped API client")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues":
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			pages++
			// One row per page exercises loading all pages before publishing metadata.
			_ = json.NewEncoder(w).Encode(map[string]any{"issues": rows[offset : offset+1], "total": len(rows)})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/projects/"):
			project := strings.Split(r.URL.Path, "/")[3]
			mode, daemonID := "worktree", "daemon"
			if project == "in-place" {
				mode = "in_place"
			}
			if project == "other-daemon" {
				daemonID = "elsewhere"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"resources": []any{map[string]any{
				"resource_type": "local_directory", "resource_ref": map[string]string{
					"local_path": repo, "daemon_id": daemonID, "execution_mode": mode,
				},
			}}})
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/metadata/"):
			parts := strings.Split(r.URL.Path, "/")
			if pages < len(rows) {
				t.Error("metadata was changed before all issue pages were loaded")
			}
			var body struct{ Value string }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if rejectPublication && parts[5] == "git_branch" {
				http.Error(w, "try again", http.StatusServiceUnavailable)
				return
			}
			for _, row := range rows {
				if row.ID == parts[3] {
					if parts[5] == "git_branch" {
						row.Metadata.Branch = body.Value
					} else {
						row.Metadata.BaseBranch = body.Value
					}
				}
			}
			writes++
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("preparation made an unexpected request (no task should start): %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	d := &Daemon{client: NewClient(srv.URL), cfg: Config{DaemonID: "daemon"}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	d.client.SetToken("daemon-user-token")
	if err := d.prepareWorkspaceIssueBranches(context.Background(), "workspace"); err != nil {
		t.Fatal(err)
	}
	reserved := git("branch", "--list", "issue/*")
	if reserved == "" {
		t.Fatal("metadata outage prevented reserving a real branch")
	}
	mu.Lock()
	rejectPublication = false
	mu.Unlock()
	if err := d.prepareWorkspaceIssueBranches(context.Background(), "workspace"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	child, root := *rows[0], *rows[1]
	beforeWrites := writes
	for _, row := range rows[2:] {
		if row.Metadata.Branch != "" {
			t.Errorf("unexpected branch for %s", row.ID)
		}
	}
	mu.Unlock()
	if root.Metadata.Branch == "" || root.Metadata.BaseBranch != "main" || child.Metadata.Branch == "" || child.Metadata.BaseBranch != root.Metadata.Branch {
		t.Fatalf("hierarchy not bound: root=%+v child=%+v", root, child)
	}
	if !strings.Contains(git("branch", "--list", "issue/*"), reserved) || git("rev-parse", child.Metadata.Branch) != git("rev-parse", "main") {
		t.Fatal("metadata retry replaced the branch or used an unrelated base")
	}
	if err := d.prepareWorkspaceIssueBranches(context.Background(), "workspace"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != beforeWrites {
		t.Fatal("already bound issues were rewritten")
	}
	if git("branch", "--show-current") != "main" || len(strings.Split(git("worktree", "list", "--porcelain"), "worktree ")) != 2 {
		t.Fatal("background preparation checked out a worktree")
	}
}

func TestIssueWorktreeParams(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, issue, parent, branch, base string
		failParent                        bool
	}{
		{"root", `{}`, "", "", "main", false},
		{"child", `{"parent_issue_id":"parent"}`, `{"metadata":{"git_branch":"issue/parent"}}`, "", "issue/parent", false},
		{"parent unbound", `{"parent_issue_id":"parent"}`, `{}`, "", "main", false},
		{"continued", `{"parent_issue_id":"parent","metadata":{"git_branch":"issue/child","git_base_branch":"issue/original-parent"}}`, "", "issue/child", "issue/original-parent", false},
		{"parent read fails", `{"parent_issue_id":"parent"}`, "", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer mat_issue" || r.Header.Get("X-Workspace-ID") != "workspace" {
					t.Error("branch binding did not use the task's workspace-scoped token")
				}
				paths = append(paths, r.URL.Path)
				switch r.URL.Path {
				case "/api/issues/child":
					_, _ = w.Write([]byte(tc.issue))
				case "/api/issues/parent":
					if tc.failParent {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					_, _ = w.Write([]byte(tc.parent))
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			d := &Daemon{client: NewClient(srv.URL)}
			d.client.SetToken("daemon-token-must-stay-private")
			params, err := d.issueWorktreeParams(context.Background(), Task{IssueID: "child", WorkspaceID: "workspace", AuthToken: "mat_issue"}, "/runtime/repo")
			if tc.failParent {
				if err == nil || !strings.Contains(err.Error(), "direct parent") {
					t.Fatalf("parent lookup failure fell back to main: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if params.IssueBranch != tc.branch || params.BaseBranch != tc.base || params.IssueID != "child" || params.LocalPath != "/runtime/repo" {
				t.Fatalf("wrong issue worktree parameters: %+v", params)
			}
			if tc.branch != "" && len(paths) != 1 {
				t.Fatalf("an existing binding was retargeted via parent lookup: %v", paths)
			}
		})
	}
}

func TestBindIssueWorktreeMetadata(t *testing.T) {
	t.Parallel()
	for _, failKey := range []string{"", "git_base_branch", "git_branch"} {
		t.Run("failure="+failKey, func(t *testing.T) {
			var writes []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.Header.Get("Authorization") != "Bearer mat_issue" {
					t.Errorf("unexpected binding request: %s %s", r.Method, r.URL.Path)
				}
				key := strings.TrimPrefix(r.URL.Path, "/api/issues/child/metadata/")
				var body struct {
					Value string `json:"value"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				writes = append(writes, key+"="+body.Value)
				if key == failKey {
					http.Error(w, "write failed", http.StatusServiceUnavailable)
					return
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			d := &Daemon{client: NewClient(srv.URL)}
			err := d.bindIssueWorktree(context.Background(), Task{IssueID: "child", WorkspaceID: "workspace", AuthToken: "mat_issue"},
				&execenv.LocalWorktree{IssueBound: true, Branch: "issue/child", BaseBranch: "issue/parent"})
			if (err != nil) != (failKey != "") {
				t.Fatalf("metadata write failure lost: %v", err)
			}
			want := []string{"git_base_branch=issue/parent", "git_branch=issue/child"}
			if failKey == "git_base_branch" {
				want = want[:1]
			}
			if !reflect.DeepEqual(writes, want) {
				t.Fatalf("metadata writes = %v, want %v", writes, want)
			}
		})
	}
}

func TestRunTaskBindsIssueWorktreeBeforeStart(t *testing.T) {
	for _, rejectMetadata := range []bool{false, true} {
		t.Run(fmt.Sprintf("reject metadata=%t", rejectMetadata), func(t *testing.T) {
			if _, err := exec.LookPath("git"); err != nil {
				t.Skip("git unavailable")
			}
			repo := t.TempDir()
			for _, args := range [][]string{
				{"init", "-b", "main"},
				{"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial"},
			} {
				if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
					t.Fatalf("git %v: %s: %v", args, out, err)
				}
			}
			metadata := map[string]string{}
			started := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/issues/issue-1":
					_, _ = w.Write([]byte(`{}`))
				case strings.Contains(r.URL.Path, "/metadata/"):
					if rejectMetadata {
						http.Error(w, "binding unavailable", http.StatusServiceUnavailable)
						return
					}
					var body struct {
						Value string `json:"value"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					metadata[strings.TrimPrefix(r.URL.Path, "/api/issues/issue-1/metadata/")] = body.Value
				case strings.HasSuffix(r.URL.Path, "/start"):
					started = true
					if metadata["git_branch"] == "" || metadata["git_base_branch"] != "main" {
						t.Errorf("agent started before issue branch metadata was ready: %v", metadata)
					}
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			d := &Daemon{
				client: NewClient(srv.URL), logger: logger,
				localPathLocks: NewLocalPathLocker(), activeEnvRoots: make(map[string]int),
				cfg: Config{DaemonID: "daemon-1", ServerBaseURL: srv.URL, WorkspacesRoot: t.TempDir(),
					Agents: map[string]AgentEntry{"claude": {Path: filepath.Join(t.TempDir(), "missing-agent")}}},
			}
			resource, err := json.Marshal(localDirectoryRef{LocalPath: repo, DaemonID: "daemon-1", ExecutionMode: localDirectoryModeWorktree})
			if err != nil {
				t.Fatal(err)
			}
			task := Task{ID: "task-1", WorkspaceID: "workspace-1", RuntimeID: "runtime-1", IssueID: "issue-1", IssueIdentifier: "MUL-1",
				AgentID: "agent-1", AuthToken: "mat_issue", Agent: &AgentData{ID: "agent-1", Name: "Worker"},
				ProjectResources: []ProjectResourceData{{ResourceType: localDirectoryResourceType, ResourceRef: resource}}}
			_, runErr := d.runTask(context.Background(), task, "claude", 0, logger)
			if rejectMetadata {
				if started || runErr == nil || !strings.Contains(runErr.Error(), "bind issue") {
					t.Fatalf("metadata failure didn't stop preparation: started=%t err=%v", started, runErr)
				}
			} else if !started {
				t.Fatalf("task never started: %v", runErr)
			}
		})
	}
}
