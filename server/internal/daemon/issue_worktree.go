package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type worktreeIssue struct {
	ID             string `json:"id"`
	Identifier     string `json:"identifier"`
	ProjectID      string `json:"project_id"`
	Status         string `json:"status"`
	StatusCategory string `json:"status_category"`
	ParentIssueID  string `json:"parent_issue_id"`
	Metadata       struct {
		Branch     string `json:"git_branch"`
		BaseBranch string `json:"git_base_branch"`
	} `json:"metadata"`
}

func (d *Daemon) issueWorktreeClient(task Task) (*cli.APIClient, error) {
	token, err := taskScopedAuthToken(task)
	if err != nil {
		return nil, err
	}
	client := cli.NewAPIClient(d.client.baseURL, task.WorkspaceID, token)
	client.HTTPClient = d.client.client
	return client, nil
}

func (d *Daemon) issueWorktreeParams(ctx context.Context, task Task, localPath string) (*execenv.LocalWorktreeParams, error) {
	params := &execenv.LocalWorktreeParams{LocalPath: localPath}
	if task.IssueID == "" {
		return params, nil
	}
	client, err := d.issueWorktreeClient(task)
	if err != nil {
		return nil, err
	}
	var issue worktreeIssue
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(task.IssueID), &issue); err != nil {
		return nil, fmt.Errorf("read issue branch binding: %w", err)
	}
	params.IssueID = task.IssueID
	params.IssueBranch = issue.Metadata.Branch
	params.BaseBranch = issue.Metadata.BaseBranch
	if params.BaseBranch == "" && issue.ParentIssueID != "" {
		var parent worktreeIssue
		if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issue.ParentIssueID), &parent); err != nil {
			return nil, fmt.Errorf("read direct parent branch binding: %w", err)
		}
		params.BaseBranch = parent.Metadata.Branch
	}
	if params.BaseBranch == "" {
		params.BaseBranch = "main"
	}
	return params, nil
}

func (d *Daemon) bindIssueWorktree(ctx context.Context, task Task, worktree *execenv.LocalWorktree) error {
	if worktree == nil || !worktree.IssueBound {
		return nil
	}
	client, err := d.issueWorktreeClient(task)
	if err != nil {
		return err
	}
	return writeIssueBranchBinding(ctx, client, task.IssueID, worktree.Branch, worktree.BaseBranch)
}

func writeIssueBranchBinding(ctx context.Context, client *cli.APIClient, issueID, branch, base string) error {
	// Publish the branch last: its presence means the complete binding is ready.
	for _, entry := range []struct{ key, value string }{
		{"git_base_branch", base},
		{"git_branch", branch},
	} {
		if err := client.PutJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/metadata/"+entry.key,
			map[string]string{"value": entry.value}, nil); err != nil {
			return fmt.Errorf("bind issue %s metadata: %w", entry.key, err)
		}
	}
	return nil
}

// Each workspace uses the existing client event stream; only reconnects need a recovery scan.
func (d *Daemon) issueWorktreeLoop(ctx context.Context) {
	watching := make(map[string]context.CancelFunc)
	defer func() {
		for _, cancel := range watching {
			cancel()
		}
	}()
	for {
		reconnected := d.reconcile.notify()
		d.mu.Lock()
		for id := range d.workspaces {
			if watching[id] == nil {
				watchCtx, cancel := context.WithCancel(ctx)
				watching[id] = cancel
				go d.watchIssueBranches(watchCtx, id)
			}
		}
		for id, cancel := range watching {
			if d.workspaces[id] == nil {
				cancel()
				delete(watching, id)
			}
		}
		d.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-reconnected:
		}
	}
}

func (d *Daemon) watchIssueBranches(ctx context.Context, workspaceID string) {
	backoff := time.Second
	for ctx.Err() == nil {
		if err := d.watchIssueBranchConnection(ctx, workspaceID); err != nil && ctx.Err() == nil {
			d.logger.Warn("issue branch event stream disconnected", "workspace_id", workspaceID, "error", err)
		}
		if sleepWithContext(ctx, backoff) != nil {
			return
		}
		backoff = min(backoff*2, taskWakeupMaxBackoff)
	}
}

func (d *Daemon) watchIssueBranchConnection(ctx context.Context, workspaceID string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	u, err := url.Parse(d.client.baseURL)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return fmt.Errorf("issue event stream requires http or https")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/ws"
	q := u.Query()
	q.Set("workspace_id", workspaceID)
	u.RawQuery = q.Encode()
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	conn.SetReadLimit(16 << 20)
	_ = conn.SetWriteDeadline(time.Now().Add(taskWakeupWriteWait))
	if err := conn.WriteJSON(map[string]any{"type": "auth", "payload": map[string]string{"token": d.client.Token()}}); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(taskWakeupWriteWait))
	var ack protocol.Message
	if err := conn.ReadJSON(&ack); err != nil {
		return err
	}
	if ack.Type != "auth_ack" {
		return fmt.Errorf("issue event stream authentication failed")
	}
	_ = conn.SetReadDeadline(time.Now().Add(taskWakeupPongWait))
	conn.SetPingHandler(func(data string) error {
		_ = conn.SetReadDeadline(time.Now().Add(taskWakeupPongWait))
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(taskWakeupWriteWait))
	})
	created := make(chan string, 64)
	go func() {
		if err := d.prepareWorkspaceIssueBranches(ctx, workspaceID); err != nil {
			d.logger.Warn("recover issue branches", "workspace_id", workspaceID, "error", err)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case id := <-created:
				if err := d.prepareCreatedIssueBranch(ctx, workspaceID, id); err != nil {
					d.logger.Warn("prepare created issue branch", "issue_id", id, "error", err)
				}
			}
		}
	}()
	for {
		var msg protocol.Message
		if err := conn.ReadJSON(&msg); err != nil {
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(taskWakeupPongWait))
		if msg.Type != protocol.EventIssueCreated {
			continue
		}
		var payload struct {
			Issue worktreeIssue `json:"issue"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.Issue.ID == "" {
			continue
		}
		select {
		case created <- payload.Issue.ID:
		default:
			return fmt.Errorf("issue event queue full; reconnect to recover branches")
		}
	}
}

func (d *Daemon) prepareWorkspaceIssueBranches(ctx context.Context, workspaceID string) error {
	client := cli.NewAPIClient(d.client.baseURL, workspaceID, d.client.Token())
	client.HTTPClient = d.client.client
	issues := make(map[string]*worktreeIssue)
	// Read all pages before metadata writes change the issue list's update ordering.
	for offset := 0; ; {
		var page struct {
			Issues []*worktreeIssue `json:"issues"`
			Total  int              `json:"total"`
		}
		if err := client.GetJSON(ctx, fmt.Sprintf("/api/issues?limit=100&offset=%d", offset), &page); err != nil {
			return err
		}
		for _, issue := range page.Issues {
			issues[issue.ID] = issue
		}
		offset += len(page.Issues)
		if offset >= page.Total {
			break
		}
		if len(page.Issues) == 0 {
			return fmt.Errorf("issue list ended before its total")
		}
	}
	return d.prepareIssueBranches(ctx, workspaceID, client, issues)
}

func (d *Daemon) prepareCreatedIssueBranch(ctx context.Context, workspaceID, issueID string) error {
	client := cli.NewAPIClient(d.client.baseURL, workspaceID, d.client.Token())
	client.HTTPClient = d.client.client
	var issue worktreeIssue
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID), &issue); err != nil {
		return err
	}
	return d.prepareIssueBranches(ctx, workspaceID, client, map[string]*worktreeIssue{issueID: &issue})
}

func (d *Daemon) prepareIssueBranches(ctx context.Context, workspaceID string, client *cli.APIClient, issues map[string]*worktreeIssue) error {
	assignments := make(map[string]*localDirectoryAssignment)
	visiting := make(map[string]bool)
	var prepare func(*worktreeIssue) error
	prepare = func(issue *worktreeIssue) error {
		category := issue.StatusCategory
		if builtIn, ok := issuestatus.CategoryForBehavior(issue.Status); ok {
			category = builtIn
		}
		if issue.Metadata.Branch != "" || issue.ProjectID == "" || category == "done" || category == "closed" || category == "cancelled" {
			return nil
		}
		if visiting[issue.ID] {
			return fmt.Errorf("issue parent cycle at %s", issue.ID)
		}
		visiting[issue.ID] = true
		defer delete(visiting, issue.ID)
		assignment, loaded := assignments[issue.ProjectID]
		if !loaded {
			var resources struct {
				Resources []ProjectResourceData `json:"resources"`
			}
			if err := client.GetJSON(ctx, "/api/projects/"+url.PathEscape(issue.ProjectID)+"/resources", &resources); err != nil {
				return err
			}
			var err error
			assignment, err = findLocalDirectoryAssignment(resources.Resources, d.cfg.DaemonID)
			if err != nil {
				return err
			}
			assignments[issue.ProjectID] = assignment
		}
		if !assignment.UsesWorktree() {
			return nil
		}
		base := issue.Metadata.BaseBranch
		if base == "" && issue.ParentIssueID != "" {
			parent := issues[issue.ParentIssueID]
			if parent == nil {
				parent = &worktreeIssue{}
				if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issue.ParentIssueID), parent); err != nil {
					return err
				}
				issues[parent.ID] = parent
			}
			if err := prepare(parent); err != nil {
				return err
			}
			base = parent.Metadata.Branch
		}
		if base == "" {
			base = "main"
		}
		branch, err := execenv.PrepareIssueBranch(execenv.LocalWorktreeParams{
			LocalPath: assignment.AbsPath, WorkspaceID: workspaceID, IssueID: issue.ID,
			ConversationKey: issue.Identifier, BaseBranch: base,
		}, d.logger)
		if err != nil {
			return err
		}
		if err := writeIssueBranchBinding(ctx, client, issue.ID, branch, base); err != nil {
			return err
		}
		issue.Metadata.Branch, issue.Metadata.BaseBranch = branch, base
		d.logger.Info("prepared issue branch", "issue_id", issue.ID, "branch", branch, "base_branch", base)
		return nil
	}
	for _, issue := range issues {
		if err := prepare(issue); err != nil {
			d.logger.Warn("prepare issue branch", "issue_id", issue.ID, "error", err)
		}
	}
	return nil
}
