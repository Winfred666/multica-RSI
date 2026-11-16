package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

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
	if code == 0 {
		if err := d.cleanRemoteSOPAnswer(ctx, task); err != nil {
			result.Content = "questionnaire cleanup: " + err.Error()
			var denied *cli.HTTPError
			if errors.As(err, &denied) && denied.Method == http.MethodPut && denied.StatusCode == http.StatusForbidden {
				result.Input["questionnaire_edit_denied"] = true
			}
		}
	}
	if err := d.client.ReportTaskMessages(ctx, task.ID, []TaskMessageData{{Seq: 1, CallID: task.TriggerCommentID, Type: "tool_use", Tool: "remote-sop", Content: task.TriggerCommentContent, CreatedAt: time.Now().UTC()}, result}); err != nil {
		d.logger.Error("remote SOP result failed", "task_id", task.ID, "error", err)
	}
	return TaskResult{Status: "completed"}, err
}

type remoteSOPCommand struct {
	Args              []string `json:"args"`
	QuestionCommentID string   `json:"que_comment_id,omitempty"`
}

func (d *Daemon) runRemoteSOP(ctx context.Context, task Task) (string, int, error) {
	name, payload, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(task.TriggerCommentContent), "!sop-remote")), " ")
	var command remoteSOPCommand
	if err := json.Unmarshal([]byte(payload), &command); err != nil {
		return "", 1, err
	}
	if command.QuestionCommentID != "" {
		var issue struct {
			Metadata map[string]string `json:"metadata"`
		}
		client := cli.NewAPIClient(d.cfg.ServerBaseURL, task.WorkspaceID, task.AuthToken)
		if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(remoteSOPIssue(task, command)), &issue); err != nil {
			return "", 1, err
		}
		var state struct {
			WorkDir string `json:"workdir"`
		}
		if err := json.Unmarshal([]byte(issue.Metadata["sop."+name]), &state); err != nil {
			return "", 1, err
		}
		task.PriorWorkDir = state.WorkDir
	}
	if task.PriorWorkDir == "" {
		return "", 1, fmt.Errorf("remote SOP requires the target working directory")
	}
	process := exec.CommandContext(ctx, "multica", append([]string{"sop", "run", name}, command.Args...)...)
	process.Dir = task.PriorWorkDir
	process.Env = append(os.Environ(), "MULTICA_AGENT_ID="+task.AgentID,
		"MULTICA_ISSUE_ID="+task.IssueID, "MULTICA_WORKSPACE_ID="+task.WorkspaceID,
		"MULTICA_TOKEN="+task.AuthToken, "MULTICA_SERVER_URL="+d.cfg.ServerBaseURL)
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

func (d *Daemon) cleanRemoteSOPAnswer(ctx context.Context, task Task) error {
	name, payload, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(task.TriggerCommentContent), "!sop-remote")), " ")
	var command remoteSOPCommand
	if err := json.Unmarshal([]byte(payload), &command); err != nil {
		return err
	}
	if command.QuestionCommentID == "" {
		return nil
	}
	client := cli.NewAPIClient(d.cfg.ServerBaseURL, task.WorkspaceID, task.AuthToken)
	type comment struct {
		ID       string `json:"id"`
		Content  string `json:"content"`
		Revision int64  `json:"revision"`
	}
	var comments []comment
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(remoteSOPIssue(task, command))+"/comments?thread="+url.QueryEscape(command.QuestionCommentID), &comments); err != nil {
		return err
	}
	for _, question := range comments {
		if question.ID != command.QuestionCommentID {
			continue
		}
		header, body, _ := strings.Cut(question.Content, "\n")
		if !strings.Contains(header, "!sop-render "+name+" ") {
			return nil
		}
		var rendered map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &rendered); err != nil {
			return err
		}
		if rendered == nil {
			return fmt.Errorf("questionnaire must contain a JSON object")
		}
		rendered["args"], _ = json.Marshal(append([]string{}, command.Args...))
		updated, err := json.Marshal(rendered)
		if err != nil {
			return err
		}
		if err := client.DeleteJSON(ctx, "/api/comments/"+url.PathEscape(task.TriggerCommentID)+"/keep-replies"); err != nil {
			return err
		}
		content := header + "\n" + string(updated)
		if err := client.PutJSON(ctx, "/api/comments/"+url.PathEscape(question.ID), map[string]any{
			"content": content, "expected_revision": question.Revision,
		}, nil); err != nil {
			return err
		}
		var forwardID string
		if raw := rendered["forwarded_comment_id"]; raw != nil {
			if err := json.Unmarshal(raw, &forwardID); err != nil {
				return err
			}
		}
		if forwardID == "" {
			return nil
		}
		_, destination, _ := strings.Cut(header, "!sop-render "+name+" ")
		var target struct {
			IssueID string `json:"issue_id"`
		}
		if err := json.Unmarshal([]byte(destination), &target); err != nil {
			return err
		}
		var forwards []comment
		if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(target.IssueID)+"/comments?thread="+url.QueryEscape(forwardID), &forwards); err != nil {
			return err
		}
		for _, forward := range forwards {
			if forward.ID != forwardID {
				continue
			}
			lines := strings.Split(forward.Content, "\n")
			for i, line := range lines {
				if link, ok := strings.CutPrefix(line, "[Question]("); ok {
					lines[i] = strings.TrimSuffix(link, ")")
				}
			}
			return client.PutJSON(ctx, "/api/comments/"+url.PathEscape(forward.ID), map[string]any{
				"content": strings.Join(lines, "\n") + "\n\n" + content, "expected_revision": forward.Revision,
			}, nil)
		}
		return fmt.Errorf("forwarding comment %s is missing", forwardID)
	}
	return nil
}
