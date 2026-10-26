package daemon

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/issuestatus"
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

// Issue creation need not schedule an agent, so branch preparation also runs independently of tasks.
func (d *Daemon) issueWorktreeLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		d.mu.Lock()
		workspaces := make([]string, 0, len(d.workspaces))
		for id := range d.workspaces {
			workspaces = append(workspaces, id)
		}
		d.mu.Unlock()
		for _, id := range workspaces {
			if ctx.Err() != nil {
				return
			}
			if err := d.prepareWorkspaceIssueBranches(ctx, id); err != nil {
				d.logger.Warn("prepare issue branches", "workspace_id", id, "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
