package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

func (d *Daemon) runRemoteSOP(ctx context.Context, request protocol.RemoteSOPRequest) (string, int, error) {
	name, payload, _ := strings.Cut(strings.TrimPrefix(request.Content, "/note !sop "), " ")
	var argv []string
	if err := json.Unmarshal([]byte(payload), &argv); err != nil {
		return "", 1, err
	}
	root := filepath.Join(d.cfg.WorkspacesRoot, "remote-sops", request.IssueID, request.AgentID)
	var skills []SkillData
	if err := json.Unmarshal(request.Skills, &skills); err != nil {
		return "", 1, err
	}
	bound := filepath.Join(root, "skills")
	for _, skill := range skills {
		dir := filepath.Join(bound, skill.Name)
		for _, file := range append([]SkillFileData{{Path: "SKILL.md", Content: skill.Content}}, skill.Files...) {
			path := filepath.Join(dir, file.Path)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				return "", 1, err
			}
			if err := os.WriteFile(path, []byte(file.Content), 0600); err != nil {
				return "", 1, err
			}
		}
	}
	work := request.WorkDir
	if info, err := os.Stat(work); err != nil || !info.IsDir() {
		work = filepath.Join(root, "workdir")
	}
	if err := os.MkdirAll(work, 0700); err != nil {
		return "", 1, err
	}
	wrapper, available := findRemoteSOP([]string{bound, filepath.Join(work, ".agents", "skills"), filepath.Join(work, ".agents")}, name)
	if wrapper == "" {
		return fmt.Sprintf("SOP %q not found. Available SOPs: %s\n", name, strings.Join(available, ", ")), 1, nil
	}
	config := filepath.Join(root, "config")
	if err := os.MkdirAll(config, 0700); err != nil {
		return "", 1, err
	}
	process := exec.CommandContext(ctx, "python3", append([]string{wrapper}, argv...)...)
	process.Dir = work
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if !strings.HasPrefix(key, "MULTICA_") {
			process.Env = append(process.Env, item)
		}
	}
	process.Env = append(process.Env, "MULTICA_TASK_ID=0", "MULTICA_AGENT_ID="+request.AgentID, "MULTICA_ISSUE_ID="+request.IssueID, "MULTICA_WORKSPACE_ID="+request.WorkspaceID, "MULTICA_TOKEN="+request.AuthToken, "MULTICA_SERVER_URL="+d.cfg.ServerBaseURL, "MULTICA_TASK_CONFIG_ROOT="+config)
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

func findRemoteSOP(roots []string, name string) (string, []string) {
	available := []string{}
	wrapper := ""
	for _, root := range roots {
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			dir := filepath.Join(root, entry.Name())
			controller, err := os.Stat(filepath.Join(dir, "controller.py"))
			if err != nil || controller.IsDir() {
				continue
			}
			script := filepath.Join(dir, "wrapper.py")
			info, err := os.Stat(script)
			if err != nil || info.IsDir() {
				continue
			}
			if !slices.Contains(available, entry.Name()) {
				available = append(available, entry.Name())
			}
			if entry.Name() == name && wrapper == "" {
				wrapper = script
			}
		}
	}
	slices.Sort(available)
	return wrapper, available
}
