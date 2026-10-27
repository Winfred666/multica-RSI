package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/processtree"
)

type issuePRBinding struct {
	ParentIssueID  string `json:"parent_issue_id"`
	Status         string `json:"status"`
	StatusCategory string `json:"status_category"`
	Metadata       struct {
		Branch string `json:"git_branch"`
		Base   string `json:"git_base_branch"`
		PRURLs string `json:"git_pr_urls"`
	} `json:"metadata"`
}

type issuePRDecision struct {
	Action string `json:"action"`
	PR     string `json:"pr"`
	Head   string `json:"head"`
	Prompt string `json:"prompt,omitempty"`
}

func parseIssuePRDecision(raw string) (issuePRDecision, error) {
	var decision issuePRDecision
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decision); err != nil {
		return decision, fmt.Errorf("decode PR decision: %w", err)
	}
	if dec.Decode(new(any)) != io.EOF {
		return decision, fmt.Errorf("expected one JSON decision")
	}
	if decision.Action != "merge" && decision.Action != "resteer" && decision.Action != "reject" {
		return decision, fmt.Errorf("decision action must be merge, resteer, or reject")
	}
	if !githubPullRequestURL.MatchString(decision.PR) || decision.Head == "" || (decision.Action == "resteer" && strings.TrimSpace(decision.Prompt) == "") {
		return decision, fmt.Errorf("decision requires pr and reviewed head; resteer also requires prompt")
	}
	if decision.Action == "reject" {
		decision.Action = "close"
	}
	return decision, nil
}

var githubPullRequestURL = regexp.MustCompile(`^https://github\.com/[^/?#]+/[^/?#]+/pull/[1-9][0-9]*$`)

// Commands inherit the daemon task's local PATH and credentials.
func localPRCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := processtree.Run(ctx, cmd, 5*time.Second); err != nil {
		return nil, fmt.Errorf("run local %s %s: %w\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	_, _ = os.Stderr.Write(stderr.Bytes())
	return stdout.Bytes(), nil
}

func runIssuePullRequestAction(ctx context.Context, client *cli.APIClient, issueID string, decision issuePRDecision) error {
	prURL, action, head := decision.PR, decision.Action, decision.Head
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
		linked, _, err := readIssuePullRequestURLs(ctx, client, issueID, issue)
		if err != nil {
			return err
		}
		if !containsPRURL(linked, prURL) {
			return fmt.Errorf("link this PR to the issue with --link before reviewing it")
		}
	}
	pr, err := readLocalPullRequest(ctx, prURL)
	if err != nil {
		return err
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
		urls, _, err := readIssuePullRequestURLs(ctx, client, issueID, issue)
		if err != nil {
			return err
		}
		if !containsPRURL(urls, prURL) {
			urls = append(urls, prURL)
		}
		data, _ := json.Marshal(urls)
		return client.PutJSON(ctx, path+"/metadata/git_pr_urls", map[string]string{"value": string(data)}, nil)
	}
	if issue.Metadata.Branch == "" || issue.Metadata.Base == "" {
		return fmt.Errorf("bind git_branch and git_base_branch with --link before reviewing this PR")
	}
	if pr.Head != head || (pr.State != "OPEN" && !(action == "merge" && pr.State == "MERGED") && !(action == "close" && pr.State == "CLOSED")) {
		return fmt.Errorf("PR head changed or PR is not open; review its current head")
	}
	args := []string{"pr", action, prURL}
	if action == "resteer" {
		args = []string{"pr", "comment", prURL, "--body", decision.Prompt}
	}
	if action == "close" && decision.Prompt != "" {
		args = append(args, "--comment", decision.Prompt)
	}
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
		}
	}
	if pr.State == "OPEN" {
		if action == "merge" && pr.Draft {
			if _, err := localPRCommand(ctx, "gh", "pr", "ready", prURL); err != nil {
				return err
			}
		}
		if _, err := localPRCommand(ctx, "gh", args...); err != nil {
			return err
		}
	}
	pr, err = readLocalPullRequest(ctx, prURL)
	if err != nil {
		return err
	}
	if pr.Head != head || (action == "merge" && pr.State != "MERGED") || (action == "close" && pr.State != "CLOSED") || (action == "resteer" && pr.State != "OPEN") {
		return fmt.Errorf("GitHub has not confirmed PR %s at the reviewed head; retry this command after it completes", action)
	}
	if action == "merge" && issue.ParentIssueID != "" {
		for _, args := range [][]string{{"fetch", "origin", "refs/heads/" + pr.Base}, {"merge", "--no-edit", "FETCH_HEAD"}} {
			if _, err := localPRCommand(ctx, "git", args...); err != nil {
				return fmt.Errorf("PR merged; updating the local parent branch failed: %w", err)
			}
		}
		if err := cleanupMergedIssueBranch(ctx, pr.Branch); err != nil {
			return fmt.Errorf("PR merged; child branch cleanup failed: %w", err)
		}
	}
	if action == "merge" {
		return completeIssueFromLocalPullRequests(ctx, client, issueID)
	}
	status := "cancelled"
	if action == "resteer" {
		status = "in_progress"
	}
	return client.PutJSON(ctx, path, map[string]string{"status": status, "handoff_note": decision.Prompt}, nil)
}

func cleanupMergedIssueBranch(ctx context.Context, branch string) error {
	data, err := localPRCommand(ctx, "git", "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return err
	}
	var path string
	for _, field := range strings.Split(string(data), "\x00") {
		if strings.HasPrefix(field, "worktree ") {
			path = strings.TrimPrefix(field, "worktree ")
		}
		if field == "branch refs/heads/"+branch {
			// Git refuses dirty worktrees; never discard unfinished child work.
			if _, err := localPRCommand(ctx, "git", "worktree", "remove", path); err != nil {
				return err
			}
		}
	}
	data, err = localPRCommand(ctx, "git", "for-each-ref", "--format=%(refname)", "refs/heads/"+branch)
	if err != nil {
		return err
	}
	for _, ref := range strings.Fields(string(data)) {
		if ref == "refs/heads/"+branch {
			if _, err := localPRCommand(ctx, "git", "branch", "--delete", "--", branch); err != nil {
				return err
			}
		}
	}
	data, err = localPRCommand(ctx, "git", "ls-remote", "--heads", "origin", "refs/heads/"+branch)
	if err != nil {
		return err
	}
	if len(data) != 0 {
		_, err = localPRCommand(ctx, "git", "push", "origin", "--delete", "refs/heads/"+branch)
	}
	return err
}

type localPullRequest struct {
	URL    string `json:"url"`
	Branch string `json:"headRefName"`
	Base   string `json:"baseRefName"`
	Head   string `json:"headRefOid"`
	State  string `json:"state"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Draft  bool   `json:"isDraft"`
	Number int    `json:"number"`
}

func readLocalPullRequest(ctx context.Context, prURL string) (localPullRequest, error) {
	var pr localPullRequest
	if !githubPullRequestURL.MatchString(prURL) {
		return pr, fmt.Errorf("expected a GitHub PR URL: %s", prURL)
	}
	data, err := localPRCommand(ctx, "gh", "pr", "view", prURL, "--json", "url,number,title,body,isDraft,headRefName,baseRefName,headRefOid,state")
	if err != nil {
		return pr, err
	}
	if err := json.Unmarshal(data, &pr); err != nil {
		return pr, fmt.Errorf("decode local gh PR: %w", err)
	}
	if !strings.EqualFold(pr.URL, prURL) || pr.Branch == "" || pr.Base == "" || pr.Head == "" ||
		(pr.State != "OPEN" && pr.State != "CLOSED" && pr.State != "MERGED") {
		return pr, fmt.Errorf("local gh returned a different or incomplete PR")
	}
	return pr, nil
}

func containsPRURL(urls []string, target string) bool {
	for _, u := range urls {
		if strings.EqualFold(u, target) {
			return true
		}
	}
	return false
}

func readIssuePullRequestURLs(ctx context.Context, client *cli.APIClient, issueID string, issue issuePRBinding) ([]string, string, error) {
	var urls []string
	if issue.Metadata.PRURLs != "" {
		if err := json.Unmarshal([]byte(issue.Metadata.PRURLs), &urls); err != nil {
			return nil, "", fmt.Errorf("decode git_pr_urls: %w", err)
		}
	}
	var linked struct {
		PRs []struct {
			URL string `json:"html_url"`
		} `json:"pull_requests"`
		AutoComplete struct {
			WorkspaceEnabled bool   `json:"workspace_enabled"`
			IssueDisabled    bool   `json:"issue_disabled"`
			Target           string `json:"target_status"`
		} `json:"auto_complete"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/pull-requests", &linked); err != nil {
		return nil, "", fmt.Errorf("read linked PRs: %w", err)
	}
	for _, pr := range linked.PRs {
		if !containsPRURL(urls, pr.URL) {
			urls = append(urls, pr.URL)
		}
	}
	target := ""
	if linked.AutoComplete.WorkspaceEnabled && !linked.AutoComplete.IssueDisabled && linked.AutoComplete.Target != "none" {
		target = linked.AutoComplete.Target
	}
	return urls, target, nil
}

func completeIssueFromLocalPullRequests(ctx context.Context, client *cli.APIClient, issueID string) error {
	path := "/api/issues/" + url.PathEscape(issueID)
	var issue issuePRBinding
	if err := client.GetJSON(ctx, path, &issue); err != nil {
		return err
	}
	if issue.Status == "done" || issue.Status == "cancelled" || issue.StatusCategory == "done" || issue.StatusCategory == "cancelled" || issue.StatusCategory == "closed" {
		return nil
	}
	urls, target, err := readIssuePullRequestURLs(ctx, client, issueID, issue)
	if err != nil {
		return err
	}
	if target == "" {
		return nil
	}
	if len(urls) == 0 {
		return fmt.Errorf("no linked PRs to confirm")
	}
	for _, u := range urls {
		pr, err := readLocalPullRequest(ctx, u)
		if err != nil {
			return err
		}
		if pr.State != "MERGED" {
			return nil
		}
	}
	return client.PutJSON(ctx, path, map[string]string{"status": target}, nil)
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
