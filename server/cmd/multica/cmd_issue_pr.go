package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
)

type issuePRBinding struct {
	ParentIssueID string `json:"parent_issue_id"`
	Metadata      struct {
		Branch string `json:"git_branch"`
		Base   string `json:"git_base_branch"`
	} `json:"metadata"`
}

var githubPullRequestURL = regexp.MustCompile(`^https://github\.com/[^/?#]+/[^/?#]+/pull/[1-9][0-9]*$`)

// Commands inherit the daemon task's local PATH and credentials.
func localPRCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run local %s: %w", name, err)
	}
	return data, nil
}

func runIssuePullRequestAction(ctx context.Context, client *cli.APIClient, issueID, prURL, action, head string) error {
	if !githubPullRequestURL.MatchString(prURL) {
		return fmt.Errorf("expected a GitHub PR URL: https://github.com/OWNER/REPO/pull/NUMBER")
	}
	path := "/api/issues/" + url.PathEscape(issueID)
	var issue issuePRBinding
	if err := client.GetJSON(ctx, path, &issue); err != nil {
		return fmt.Errorf("read issue branch binding: %w", err)
	}
	if action != "link" {
		if err := requireIssuePRReviewer(ctx, client, issue.ParentIssueID); err != nil {
			return err
		}
		var linked struct {
			PRs []struct {
				URL string `json:"html_url"`
			} `json:"pull_requests"`
		}
		if err := client.GetJSON(ctx, path+"/pull-requests", &linked); err != nil {
			return fmt.Errorf("read linked PRs: %w", err)
		}
		found := false
		for _, pr := range linked.PRs {
			found = found || strings.EqualFold(pr.URL, prURL)
		}
		if !found {
			return fmt.Errorf("link this PR to the issue with --link before reviewing it")
		}
	}
	data, err := localPRCommand(ctx, "gh", "pr", "view", prURL, "--json", "url,headRefName,baseRefName,headRefOid,state")
	if err != nil {
		return err
	}
	var pr struct {
		URL    string `json:"url"`
		Branch string `json:"headRefName"`
		Base   string `json:"baseRefName"`
		Head   string `json:"headRefOid"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(data, &pr); err != nil {
		return fmt.Errorf("decode local gh PR: %w", err)
	}
	if !strings.EqualFold(pr.URL, prURL) || pr.Branch == "" || pr.Base == "" || pr.Head == "" ||
		(pr.State != "OPEN" && pr.State != "CLOSED" && pr.State != "MERGED") {
		return fmt.Errorf("local gh returned a different or incomplete PR")
	}
	base := issue.Metadata.Base
	if base == "" {
		base = "main"
		if issue.ParentIssueID != "" {
			var parent issuePRBinding
			if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issue.ParentIssueID), &parent); err != nil {
				return fmt.Errorf("read direct parent branch: %w", err)
			}
			if parent.Metadata.Branch != "" {
				base = parent.Metadata.Branch
			}
		}
	}
	if (issue.Metadata.Branch != "" && issue.Metadata.Branch != pr.Branch) || base != pr.Base {
		return fmt.Errorf("PR must use the issue's bound branch and direct parent base (%s)", base)
	}
	if action == "link" {
		// Fill missing metadata before linking an existing PR; never overwrite a binding.
		for _, entry := range []struct{ key, value, current string }{
			{"git_base_branch", pr.Base, issue.Metadata.Base},
			{"git_branch", pr.Branch, issue.Metadata.Branch},
		} {
			if entry.current == "" {
				if err := client.PutJSON(ctx, path+"/metadata/"+entry.key, map[string]string{"value": entry.value}, nil); err != nil {
					return fmt.Errorf("bind issue %s before linking PR: %w", entry.key, err)
				}
			}
		}
		return nil
	}
	if issue.Metadata.Branch == "" || issue.Metadata.Base == "" {
		return fmt.Errorf("bind git_branch and git_base_branch with --link before reviewing this PR")
	}
	if pr.Head != head || pr.State != "OPEN" {
		return fmt.Errorf("PR head changed or PR is not open; review its current head")
	}
	args := []string{"pr", action, prURL}
	if action == "merge" {
		args = append(args, "--merge", "--match-head-commit", head)
		if issue.ParentIssueID != "" {
			branch, err := localPRCommand(ctx, "git", "branch", "--show-current")
			if err != nil {
				return err
			}
			if strings.TrimSpace(string(branch)) != pr.Base {
				return fmt.Errorf("accept the child's PR from its parent branch %q", pr.Base)
			}
			args = append(args, "--delete-branch")
		}
	}
	if _, err := localPRCommand(ctx, "gh", args...); err != nil {
		return err
	}
	if action == "merge" && issue.ParentIssueID != "" {
		for _, args := range [][]string{{"fetch", "origin", "refs/heads/" + pr.Base}, {"merge", "--no-edit", "FETCH_HEAD"}} {
			if _, err := localPRCommand(ctx, "git", args...); err != nil {
				return fmt.Errorf("PR merged; updating the local parent branch failed: %w", err)
			}
		}
	}
	return nil
}

func requireIssuePRReviewer(ctx context.Context, client *cli.APIClient, parentID string) error {
	if client.TaskID == "" && client.AgentID == "" && !strings.HasPrefix(client.Token, cli.TaskTokenPrefix) {
		return nil
	}
	if parentID != "" && client.TaskID != "" {
		runs, err := fetchTaskRunCandidatesForIssue(ctx, client, parentID)
		if err != nil {
			return fmt.Errorf("read direct parent runs: %w", err)
		}
		for _, run := range runs {
			if run.ID == client.TaskID {
				return nil
			}
		}
	}
	return fmt.Errorf("PR review requires the direct parent issue's run, or a human reviewer for a root issue")
}
