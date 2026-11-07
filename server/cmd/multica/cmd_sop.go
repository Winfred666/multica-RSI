package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

const sopCallPrefix = "/note /sop "

var sopNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

var sopCmd = &cobra.Command{
	Use:   "sop",
	Short: "Register and call named host procedures through issue comments",
	Long: "Register an executable on its host, then run serve with a dedicated member profile. " +
		"Calls and results use /note comments and do not dispatch agents. " +
		"Only registered names and declared string inputs can run. " +
		"Issue members who can post comments can request these procedures.",
	Example: "  multica --profile sop-host sop register R-42 summarize --executable /opt/sops/summarize --input topic\n" +
		"  multica --profile sop-host sop serve R-42\n" +
		"  multica sop call R-42 summarize --input topic=release",
}

var sopRegisterCmd = &cobra.Command{
	Use:   "register <issue-id> <name>",
	Short: "Register a local executable and its required input names for an issue",
	Long: "Registers an absolute executable path in this CLI profile, bound to the current server, workspace and issue. " +
		"The executable takes no arguments. It receives JSON on stdin with issue_id, comment_id and inputs, " +
		"runs in a private invocation directory, and writes its result to stdout. " +
		"Use a standalone SOP entry point; existing active-task SOP state is not reused. Restart serve after registration changes.",
	Args: exactArgs(2),
	RunE: runSOPRegister,
}

var sopCallCmd = &cobra.Command{
	Use:   "call <issue-id> <name>",
	Short: "Request a registered SOP on another issue without dispatching its agent",
	Long: "Posts a top-level /note /sop request using the caller's normal API credentials. " +
		"The target host must run sop serve. Its result is a reply under the request. " +
		"Inputs are data, never command-line arguments. Names and input declarations are checked by the target host.",
	Args: exactArgs(2),
	RunE: runSOPCall,
}

var sopServeCmd = &cobra.Command{
	Use:   "serve <issue-id>",
	Short: "Listen for SOP calls on one issue using this host's registrations",
	Long: "Polls the existing comment API and executes registered SOPs sequentially with a dedicated member profile. " +
		"Agent task credentials are refused. Each request has a private directory under the profile's sop/runs. " +
		"Completed requests are not executed again after restart. An interrupted execution requires manual reconciliation; " +
		"submit a new call only after checking its directory. Result delivery retries on restart without rerunning the executable.",
	Args: exactArgs(1),
	RunE: runSOPServe,
}

func init() {
	sopRegisterCmd.Flags().String("executable", "", "Absolute path to the standalone SOP executable (required)")
	sopRegisterCmd.Flags().StringSlice("input", nil, "Required string input names (comma-separated or repeated)")
	sopCallCmd.Flags().StringArray("input", nil, "Input name=value (repeatable)")
	sopServeCmd.Flags().Duration("poll-interval", 2*time.Second, "Comment polling interval")
	sopServeCmd.Flags().Duration("timeout", time.Minute, "Maximum execution time per invocation")
	sopCmd.AddCommand(sopRegisterCmd, sopCallCmd, sopServeCmd)
}

type sopRegistration struct {
	ServerURL   string   `json:"server_url"`
	WorkspaceID string   `json:"workspace_id"`
	IssueID     string   `json:"issue_id"`
	Name        string   `json:"name"`
	Executable  string   `json:"executable"`
	Inputs      []string `json:"inputs"`
}

type sopRequest struct {
	Name   string            `json:"name"`
	Inputs map[string]string `json:"inputs"`
}

type sopComment struct {
	ID        string  `json:"id"`
	Content   string  `json:"content"`
	ParentID  *string `json:"parent_id"`
	DeletedAt *string `json:"deleted_at"`
}

func sopLocalClient(cmd *cobra.Command) (*cli.APIClient, error) {
	if err := requireHumanLocalCommand("sop " + cmd.Name()); err != nil {
		return nil, err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, err
	}
	if client.Token == "" || strings.HasPrefix(client.Token, cli.TaskTokenPrefix) {
		return nil, fmt.Errorf("SOP hosting requires a dedicated member profile, not an agent task token")
	}
	return client, nil
}

func sopStateDir(cmd *cobra.Command) (string, error) {
	dir, err := cli.ProfileDir(resolveProfile(cmd))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sop"), nil
}

func readSOPRegistry(dir string) ([]sopRegistration, error) {
	var entries []sopRegistration
	data, err := os.ReadFile(filepath.Join(dir, "registry.json"))
	if errors.Is(err, os.ErrNotExist) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &entries)
	return entries, err
}

func writeSOPJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".sop-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func runSOPRegister(cmd *cobra.Command, args []string) error {
	if !sopNamePattern.MatchString(args[1]) {
		return fmt.Errorf("invalid SOP name: use lowercase letters, digits, underscores or hyphens, starting with a letter")
	}
	executable, _ := cmd.Flags().GetString("executable")
	if !filepath.IsAbs(executable) {
		return fmt.Errorf("--executable must be an absolute path")
	}
	if _, err := exec.LookPath(executable); err != nil {
		return fmt.Errorf("SOP executable: %w", err)
	}
	inputs, _ := cmd.Flags().GetStringSlice("input")
	seen := make(map[string]bool)
	for _, name := range inputs {
		if !sopNamePattern.MatchString(name) || seen[name] {
			return fmt.Errorf("invalid or duplicate input name %q", name)
		}
		seen[name] = true
	}
	client, err := sopLocalClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	issue, err := fetchIssueRef(ctx, client, args[0])
	if err != nil {
		return err
	}
	dir, err := sopStateDir(cmd)
	if err != nil {
		return err
	}
	entries, err := readSOPRegistry(dir)
	if err != nil {
		return err
	}
	entry := sopRegistration{client.BaseURL, client.WorkspaceID, issue.ID, args[1], executable, inputs}
	updated := false
	for i, old := range entries {
		if old.ServerURL == entry.ServerURL && old.WorkspaceID == entry.WorkspaceID && old.IssueID == entry.IssueID && old.Name == entry.Name {
			entries[i], updated = entry, true
		}
	}
	if !updated {
		entries = append(entries, entry)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := writeSOPJSON(filepath.Join(dir, "registry.json"), entries); err != nil {
		return err
	}
	return cli.PrintJSON(cmd.OutOrStdout(), entry)
}

func runSOPCall(cmd *cobra.Command, args []string) error {
	if !sopNamePattern.MatchString(args[1]) {
		return fmt.Errorf("invalid SOP name")
	}
	request := sopRequest{Name: args[1], Inputs: make(map[string]string)}
	inputs, _ := cmd.Flags().GetStringArray("input")
	for _, input := range inputs {
		name, value, ok := strings.Cut(input, "=")
		if _, duplicate := request.Inputs[name]; !ok || !sopNamePattern.MatchString(name) || duplicate {
			return fmt.Errorf("input must be a unique name=value pair: %q", input)
		}
		request.Inputs[name] = value
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	issue, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(issue.ID)+"/comments", map[string]string{"content": sopCallPrefix + string(data)}, &result); err != nil {
		return err
	}
	return cli.PrintJSON(cmd.OutOrStdout(), result)
}

func runSOPServe(cmd *cobra.Command, args []string) error {
	interval, _ := cmd.Flags().GetDuration("poll-interval")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	if interval <= 0 || timeout <= 0 {
		return fmt.Errorf("--poll-interval and --timeout must be positive")
	}
	client, err := sopLocalClient(cmd)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	issue, err := fetchIssueRef(ctx, client, args[0])
	if err != nil {
		return err
	}
	dir, err := sopStateDir(cmd)
	if err != nil {
		return err
	}
	entries, err := readSOPRegistry(dir)
	if err != nil {
		return err
	}
	registered := make(map[string]sopRegistration)
	for _, entry := range entries {
		if entry.ServerURL == client.BaseURL && entry.WorkspaceID == client.WorkspaceID && entry.IssueID == issue.ID {
			registered[entry.Name] = entry
		}
	}
	if len(registered) == 0 {
		return fmt.Errorf("no SOPs registered for this server, workspace and issue in this profile")
	}
	if err := os.MkdirAll(filepath.Join(dir, "runs"), 0700); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Serving %d SOP(s) on %s; state: %s\n", len(registered), issue.Display, dir)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := pollSOPCalls(ctx, client, issue.ID, registered, dir, timeout); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func pollSOPCalls(ctx context.Context, client *cli.APIClient, issueID string, registered map[string]sopRegistration, dir string, timeout time.Duration) error {
	var comments []sopComment
	headers, err := client.GetJSONWithHeaders(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments?roots_only=true", &comments)
	if err != nil {
		return err
	}
	// shortcut: scan root comments; add paged discovery when issues exceed the API's root limit.
	if headers.Get("X-Comments-Truncated") == "true" {
		return fmt.Errorf("SOP comment scan exceeded the API root limit; use an issue with fewer root comments")
	}
	for _, comment := range comments {
		if comment.DeletedAt != nil || comment.ParentID != nil || !strings.HasPrefix(comment.Content, sopCallPrefix) {
			continue
		}
		if err := handleSOPCall(ctx, client, issueID, comment, registered, dir, timeout); err != nil {
			return err
		}
	}
	return nil
}

func decodeSOPRequest(content string, registered map[string]sopRegistration) (sopRequest, sopRegistration, error) {
	var request sopRequest
	decoder := json.NewDecoder(strings.NewReader(strings.TrimPrefix(content, sopCallPrefix)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, sopRegistration{}, fmt.Errorf("invalid SOP request: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return request, sopRegistration{}, fmt.Errorf("SOP request must contain one JSON object")
	}
	if !sopNamePattern.MatchString(request.Name) {
		return request, sopRegistration{}, fmt.Errorf("invalid SOP name")
	}
	entry, ok := registered[request.Name]
	if !ok {
		return request, entry, fmt.Errorf("unregistered SOP %q", request.Name)
	}
	if len(request.Inputs) != len(entry.Inputs) {
		return request, entry, fmt.Errorf("SOP %q requires exactly these inputs: %s", request.Name, strings.Join(entry.Inputs, ", "))
	}
	for _, name := range entry.Inputs {
		if _, ok := request.Inputs[name]; !ok {
			return request, entry, fmt.Errorf("SOP %q requires input %q", request.Name, name)
		}
	}
	return request, entry, nil
}

func handleSOPCall(ctx context.Context, client *cli.APIClient, issueID string, comment sopComment, registered map[string]sopRegistration, dir string, timeout time.Duration) error {
	runDir := filepath.Join(dir, "runs", comment.ID)
	if _, err := os.Stat(filepath.Join(runDir, "posted.json")); err == nil {
		return nil
	}
	resultPath := filepath.Join(runDir, "result.json")
	var content string
	data, err := os.ReadFile(resultPath)
	if err == nil {
		if err := json.Unmarshal(data, &content); err != nil {
			return err
		}
	} else {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Mkdir(runDir, 0700); err != nil {
			return fmt.Errorf("SOP invocation needs reconciliation in %s: %w", runDir, err)
		}
		request, entry, requestErr := decodeSOPRequest(comment.Content, registered)
		if requestErr != nil {
			content = "/note SOP rejected: " + requestErr.Error()
		} else {
			content = executeSOP(ctx, entry, request, issueID, comment.ID, runDir, timeout)
		}
		if err := writeSOPJSON(resultPath, content); err != nil {
			return err
		}
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments", map[string]string{"content": content, "parent_id": comment.ID}, &result); err != nil {
		return err
	}
	return writeSOPJSON(filepath.Join(runDir, "posted.json"), result)
}

func executeSOP(ctx context.Context, entry sopRegistration, request sopRequest, issueID, commentID, runDir string, timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	data, err := json.Marshal(map[string]any{"issue_id": issueID, "comment_id": commentID, "inputs": request.Inputs})
	if err != nil {
		return "/note SOP failed: " + err.Error()
	}
	process := exec.CommandContext(ctx, entry.Executable)
	process.Dir = runDir
	// A standalone invocation must not inherit any active agent/task identity or credentials.
	for _, env := range os.Environ() {
		name, _, _ := strings.Cut(env, "=")
		if !strings.HasPrefix(name, "MULTICA_") && name != "TMPDIR" && name != "TMP" && name != "TEMP" {
			process.Env = append(process.Env, env)
		}
	}
	process.Env = append(process.Env, "TMPDIR="+runDir, "TMP="+runDir, "TEMP="+runDir)
	process.Stdin = bytes.NewReader(data)
	var output sopOutput
	process.Stdout, process.Stderr = &output, &output
	process.WaitDelay = time.Second
	err = process.Run()
	status := "completed"
	if err != nil {
		status = "failed: " + err.Error()
	}
	text := strings.ToValidUTF8(output.String(), "?")
	if output.truncated {
		text += "\n[output truncated at 32 KiB]"
	}
	return fmt.Sprintf("/note SOP %s %s\n\n%s", entry.Name, status, text)
}

// Keep native replies below the comment API's 64 KiB limit.
type sopOutput struct {
	bytes.Buffer
	truncated bool
}

func (b *sopOutput) Write(p []byte) (int, error) {
	const limit = 32 * 1024
	n := len(p)
	if n > limit-b.Len() {
		b.truncated = true
	}
	if remaining := limit - b.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.Buffer.Write(p)
	}
	return n, nil
}
