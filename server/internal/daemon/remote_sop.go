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

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (d *Daemon) executeRemoteSOPs(ctx context.Context, runtimeID string) {
	var requests []protocol.RemoteSOPRequest
	if err := d.client.postJSON(ctx, "/api/daemon/runtimes/"+url.PathEscape(runtimeID)+"/remote-sops/take", nil, &requests); err != nil {
		d.logger.Debug("remote SOP poll failed", "error", err)
		return
	}
	for _, request := range requests {
		d.executeRemoteSOP(ctx, request)
	}
}

// Remote SOPs run directly from heartbeat actions, outside agent task execution.
func (d *Daemon) executeRemoteSOP(ctx context.Context, request protocol.RemoteSOPRequest) {
	output, code, err := d.runRemoteSOP(ctx, request)
	diagnostic := ""
	if err != nil {
		diagnostic = err.Error()
	}
	result := TaskMessageData{Seq: 2, CallID: request.ID, Type: "tool_result", Tool: "remote-sop", Output: output, Content: diagnostic, Input: map[string]any{"exit_code": code}, CreatedAt: time.Now().UTC()}
	if err := d.client.ReportTaskMessages(ctx, request.ID, []TaskMessageData{result}); err != nil {
		d.logger.Error("remote SOP result failed", "request_id", request.ID, "error", err)
	}
}

type remoteSOPCommand struct {
	Args              []string `json:"args"`
	QuestionCommentID string   `json:"que_comment_id,omitempty"`
}

func (d *Daemon) runRemoteSOP(ctx context.Context, request protocol.RemoteSOPRequest) (string, int, error) {
	name, payload, _ := strings.Cut(strings.TrimPrefix(request.Content, "/note !sop-remote "), " ")
	var command remoteSOPCommand
	if err := json.Unmarshal([]byte(payload), &command); err != nil {
		return "", 1, err
	}
	if request.WorkDir == "" {
		return "", 1, fmt.Errorf("remote SOP requires the target working directory")
	}
	agentID := ""
	if request.CallerType == "agent" {
		agentID = request.CallerID
	}
	process := exec.CommandContext(ctx, "multica", append([]string{"sop", "run", name}, command.Args...)...)
	process.Dir = request.WorkDir
	process.Env = append(os.Environ(), "MULTICA_AGENT_ID="+agentID,
		"MULTICA_ISSUE_ID="+request.IssueID, "MULTICA_WORKSPACE_ID="+request.WorkspaceID,
		"MULTICA_TOKEN="+request.AuthToken, "MULTICA_SERVER_URL="+d.cfg.ServerBaseURL)
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
