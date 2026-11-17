package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

// TODO: Stage 4: SoP as first-class member: directly use a special ACP command executor inside the daemon.
func (d *Daemon) executeRemoteSOP(ctx context.Context, task Task) (TaskResult, error) {
	if _, err := d.client.StartTask(ctx, task); err != nil {
		return TaskResult{}, err
	}
	output, code, err := d.runRemoteSOP(ctx, task)
	diagnostic := ""
	if err != nil {
		diagnostic = err.Error()
	}
	result := TaskMessageData{Seq: 2, CallID: task.TriggerCommentID, Type: "tool_result", Tool: "remote-sop", Output: output, Content: diagnostic, Input: map[string]any{"exit_code": code}, CreatedAt: time.Now().UTC()}
	if err != nil {
		d.reportRemoteSOPError(ctx, task, output, err)
	}

	if err := d.client.ReportTaskMessages(ctx, task.ID, []TaskMessageData{{Seq: 1, CallID: task.TriggerCommentID, Type: "tool_use", Tool: "remote-sop", Content: task.TriggerCommentContent, CreatedAt: time.Now().UTC()}, result}); err != nil {
		d.logger.Error("remote SOP result failed", "task_id", task.ID, "error", err)
	}
	return TaskResult{Status: "completed"}, err
}

// TODO: Stage 4: SoP as first-class member: use a special ACP command executor inside the daemon.
func (d *Daemon) reportRemoteSOPError(ctx context.Context, task Task, output string, err error) {
	client := cli.NewAPIClient(d.cfg.ServerBaseURL, task.WorkspaceID, d.client.token)
	client.AgentID = task.AgentID
	if postErr := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(task.IssueID)+"/comments", map[string]any{"content": "/note SoP command failed:\n" + output + "\n" + err.Error()}, nil); postErr != nil {
		d.logger.Error("remote SOP exception comment failed", "task_id", task.ID, "error", postErr)
	}
}

type remoteSOPCommand struct {
	Args []string `json:"args"`
}

// TODO: Stage 4: SoP as first-class member: use a special ACP command executor inside the daemon.
func (d *Daemon) runRemoteSOP(ctx context.Context, task Task) (string, int, error) {
	name, payload, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(task.TriggerCommentContent), "!sop-remote")), " ")
	var command remoteSOPCommand
	if err := json.Unmarshal([]byte(payload), &command); err != nil {
		return "", 1, err
	}
	var issue struct {
		Metadata map[string]string `json:"metadata"`
	}
	client := cli.NewAPIClient(d.cfg.ServerBaseURL, task.WorkspaceID, d.client.token)
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(remoteSOPIssue(task, command)), &issue); err != nil {
		return "", 1, err
	}
	var state struct {
		WorkDir string `json:"workdir"`
	}
	if err := json.Unmarshal([]byte(issue.Metadata["sop."+name]), &state); err != nil {
		return "", 1, err
	}
	if state.WorkDir == "" {
		return "", 1, fmt.Errorf("remote SOP requires the target working directory")
	}
	process := exec.CommandContext(ctx, "multica", append([]string{"sop", "run", name}, command.Args...)...)
	process.Dir = state.WorkDir
	process.Env = append(os.Environ(), "MULTICA_AGENT_ID="+task.AgentID,
		"MULTICA_ISSUE_ID="+task.IssueID, "MULTICA_WORKSPACE_ID="+task.WorkspaceID,
		"MULTICA_TOKEN="+d.client.token, "MULTICA_SERVER_URL="+d.cfg.ServerBaseURL)
	output, err := process.CombinedOutput()
	code := 0
	if err != nil {
		code = 1
		if process.ProcessState != nil && process.ProcessState.ExitCode() > 0 {
			code = process.ProcessState.ExitCode()
		}
	}
	return string(output), code, err
}

func remoteSOPIssue(task Task, command remoteSOPCommand) string {
	issue := task.IssueID
	for i, arg := range command.Args {
		if arg == "--issue" && i+1 < len(command.Args) {
			issue = command.Args[i+1]
		}
		if strings.HasPrefix(arg, "--issue=") {
			issue = strings.TrimPrefix(arg, "--issue=")
		}
	}
	return issue
}

// TODO: Stage 4: SoP as first-class member: use a special ACP command executor inside the daemon.
func (d *Daemon) cleanRemoteSOPAnswer(ctx context.Context, task Task) error {
	client := cli.NewAPIClient(d.cfg.ServerBaseURL, task.WorkspaceID, d.client.token)
	return client.DeleteJSON(ctx, "/api/comments/"+url.PathEscape(task.TriggerCommentID)+"/keep-replies")
}
