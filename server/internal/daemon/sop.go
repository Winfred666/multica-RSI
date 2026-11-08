package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/util"
)

const sopCallPrefix = "/sop "

type sopTaskKey struct{}

type sopRequest struct {
	Name   string                     `json:"name"`
	Inputs map[string]json.RawMessage `json:"inputs"`
}

type sopResult struct {
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

func decodeSOPRequest(content string, entries []cli.SOPRegistration, task Task, serverURL string) (sopRequest, cli.SOPRegistration, error) {
	var request sopRequest
	payload := strings.TrimPrefix(strings.TrimSpace(content), sopCallPrefix)
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, cli.SOPRegistration{}, err
	}
	// Routing mentions may follow the JSON envelope; they are not procedure inputs.
	if strings.TrimSpace(util.MentionRe.ReplaceAllString(payload[decoder.InputOffset():], "")) != "" {
		return request, cli.SOPRegistration{}, fmt.Errorf("SOP request must contain one JSON object and optional routing mentions")
	}
	for _, entry := range entries {
		if entry.ServerURL != serverURL || entry.WorkspaceID != task.WorkspaceID || entry.IssueID != task.IssueID || entry.Name != request.Name {
			continue
		}
		if len(request.Inputs) != len(entry.Inputs) {
			return request, entry, fmt.Errorf("SOP %q requires inputs: %s", request.Name, strings.Join(entry.Inputs, ", "))
		}
		for _, name := range entry.Inputs {
			if _, ok := request.Inputs[name]; !ok {
				return request, entry, fmt.Errorf("SOP %q requires input %q", request.Name, name)
			}
		}
		return request, entry, nil
	}
	return request, cli.SOPRegistration{}, fmt.Errorf("unregistered SOP %q on this host, server, workspace and issue", request.Name)
}

// short circuit steering: run the registered SOP and reply before sending input to a model.
// A failed call falls through to the existing agent path with its diagnostic.
func (d *Daemon) interceptSOPComment(ctx context.Context, task Task, commentID, content string) (bool, error) {
	if !strings.HasPrefix(strings.TrimSpace(content), sopCallPrefix) {
		return false, nil
	}
	token, err := taskScopedAuthToken(task)
	if err != nil {
		return false, err
	}
	if task.IssueID == "" || task.WorkspaceID == "" || commentID == "" {
		return false, fmt.Errorf("SOP requires an issue comment")
	}
	dir, err := cli.SOPStateDir(d.cfg.Profile)
	if err != nil {
		return false, err
	}
	entries, err := cli.ReadSOPRegistry(dir)
	if err != nil {
		return false, err
	}
	request, entry, err := decodeSOPRequest(content, entries, task, d.cfg.ServerBaseURL)
	if err != nil {
		return false, err
	}
	// One comment may reach multiple local recipients. Serialize the durable claim.
	d.sopMu.Lock()
	defer d.sopMu.Unlock()
	runDir := filepath.Join(dir, "runs", commentID)
	if _, err := os.Stat(filepath.Join(runDir, "posted.json")); err == nil {
		return true, nil
	}
	resultPath := filepath.Join(runDir, "result.json")
	var result sopResult
	data, err := os.ReadFile(resultPath)
	if err == nil {
		if err := json.Unmarshal(data, &result); err != nil {
			return false, err
		}
	} else {
		if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		if err := os.MkdirAll(filepath.Dir(runDir), 0700); err != nil {
			return false, err
		}
		if err := os.Mkdir(runDir, 0700); err != nil {
			return false, fmt.Errorf("reconcile interrupted SOP in %s: %w", runDir, err)
		}
		result = executeSOP(ctx, entry, request, task, commentID, runDir, d.cfg.ServerBaseURL, token)
		if err := cli.WriteSOPJSON(resultPath, result); err != nil {
			return false, err
		}
	}
	if result.Error != "" {
		return false, fmt.Errorf("%s (saved output: %s)", result.Error, resultPath)
	}
	client := cli.NewAPIClient(d.cfg.ServerBaseURL, task.WorkspaceID, token)
	var reply map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+task.IssueID+"/comments", map[string]string{"content": result.Content, "parent_id": commentID}, &reply); err != nil {
		return false, fmt.Errorf("SOP completed; reply failed (saved output: %s): %w", resultPath, err)
	}
	if err := cli.WriteSOPJSON(filepath.Join(runDir, "posted.json"), reply); err != nil {
		return false, err
	}
	return true, nil
}

func executeSOP(ctx context.Context, entry cli.SOPRegistration, request sopRequest, task Task, commentID, runDir, serverURL, token string) sopResult {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	data, err := json.Marshal(map[string]any{"issue_id": task.IssueID, "comment_id": commentID, "inputs": request.Inputs})
	if err != nil {
		return sopResult{Error: err.Error()}
	}
	process := exec.CommandContext(ctx, entry.Executable, entry.Args...)
	process.Dir = runDir
	for _, env := range os.Environ() {
		name, _, _ := strings.Cut(env, "=")
		if !strings.HasPrefix(name, "MULTICA_") && name != "TMPDIR" && name != "TMP" && name != "TEMP" {
			process.Env = append(process.Env, env)
		}
	}
	configDir := filepath.Join(runDir, "config")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return sopResult{Error: err.Error()}
	}
	process.Env = append(process.Env, "MULTICA_SERVER_URL="+serverURL, "MULTICA_WORKSPACE_ID="+task.WorkspaceID, "MULTICA_TOKEN="+token,
		"MULTICA_TASK_ID="+task.ID, "MULTICA_AGENT_ID="+task.AgentID, "MULTICA_ISSUE_ID="+task.IssueID, "MULTICA_TASK_CONFIG_ROOT="+configDir,
		"TMPDIR="+runDir, "TMP="+runDir, "TEMP="+runDir)
	process.Stdin = bytes.NewReader(data)
	var output sopOutput
	process.Stdout, process.Stderr = &output, &output
	process.WaitDelay = time.Second
	err = process.Run()
	text := strings.ToValidUTF8(output.String(), "?")
	if output.truncated {
		text += "\n[output truncated at 32 KiB]"
	}
	if err != nil {
		return sopResult{Content: text, Error: fmt.Sprintf("SOP %s failed: %v", entry.Name, err)}
	}
	return sopResult{Content: fmt.Sprintf("/note SOP %s completed\n\n%s", entry.Name, text)}
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
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}

func sopFallbackContent(content string, err error) string {
	return content + "\n\n[Daemon SOP call failed; handle this request.] " + err.Error()
}

func (d *Daemon) interceptTaskSOPs(ctx context.Context, task *Task) bool {
	if task.TriggerCommentID == "" {
		return false
	}
	allHandled := len(task.CoalescedCommentIDs) == len(task.CoalescedComments)
	for i := range task.CoalescedComments {
		comment := &task.CoalescedComments[i]
		handled, err := d.interceptSOPComment(ctx, *task, comment.ID, comment.Content)
		if err != nil {
			comment.Content = sopFallbackContent(comment.Content, err)
		}
		if handled {
			comment.Content = "/note SOP request already completed by the daemon; its result is in the reply."
		}
		allHandled = allHandled && handled
	}
	handled, err := d.interceptSOPComment(ctx, *task, task.TriggerCommentID, task.TriggerCommentContent)
	if err != nil {
		task.TriggerCommentContent = sopFallbackContent(task.TriggerCommentContent, err)
	}
	if handled {
		task.TriggerCommentContent = "/note SOP request already completed by the daemon; its result is in the reply."
	}
	return allHandled && handled
}
