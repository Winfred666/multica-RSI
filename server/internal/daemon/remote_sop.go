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
	"sync/atomic"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/util"
)

const remoteSOPPrefix = "!sop "

type remoteSOPContextKey struct{}

type remoteSOPContext struct {
	task     Task
	env      *execenv.Environment
	provider string
	seq      *atomic.Int32
}

func (d *Daemon) interceptRemoteSOP(ctx context.Context, state remoteSOPContext, commentID, content string) (bool, error) {
	if !strings.HasPrefix(content, remoteSOPPrefix) {
		return false, nil
	}
	report := func(message TaskMessageData) error {
		message.Seq = int(state.seq.Add(1))
		message.CallID, message.Tool, message.CreatedAt = commentID, "remote-sop", time.Now().UTC()
		return d.client.ReportTaskMessages(ctx, state.task.ID, []TaskMessageData{message})
	}
	if err := report(TaskMessageData{Type: "tool_use", Content: content, Input: map[string]any{"command": content}}); err != nil {
		return true, err
	}
	output, diagnostic, code := "", "", 0
	consumePath := "/api/daemon/tasks/" + url.PathEscape(state.task.ID) + "/remote-sop/consume?comment_id=" + url.QueryEscape(commentID)
	if err := d.client.postJSON(ctx, consumePath, nil, nil); err != nil {
		diagnostic, code = err.Error(), 1
	} else {
		name, payload, _ := strings.Cut(strings.TrimPrefix(content, remoteSOPPrefix), " ")
		var argv []string
		decoder := json.NewDecoder(strings.NewReader(payload))
		err := decoder.Decode(&argv)
		if err == nil && strings.TrimSpace(util.MentionRe.ReplaceAllString(payload[decoder.InputOffset():], "")) != "" {
			err = fmt.Errorf("expected !sop <name> <JSON argument array>")
		}
		if err != nil {
			diagnostic, code = err.Error(), 1
		} else {
			wrapper, available := findRemoteSOP(state.env, state.provider, name)
			if wrapper == "" {
				output, code = fmt.Sprintf("SOP %q not found. Available SOPs: %s\n", name, strings.Join(available, ", ")), 1
			} else {
				process := exec.CommandContext(ctx, "python3", append([]string{wrapper}, argv...)...)
				process.Dir = state.env.WorkDir
				remoteTask := state.task
				remoteTask.ID = "0"
				env := taskMulticaEnvironment(remoteTask, state.provider, "", state.task.AuthToken, state.env.MulticaConfigRoot, d.cfg.WorkspacesRoot, d.cfg.ServerBaseURL, 0, 0, state.env.RootDir)
				for _, item := range os.Environ() {
					key, _, _ := strings.Cut(item, "=")
					if !strings.HasPrefix(key, "MULTICA_") {
						process.Env = append(process.Env, item)
					}
				}
				for key, value := range env {
					process.Env = append(process.Env, key+"="+value)
				}
				data, err := process.CombinedOutput()
				output = string(data)
				if err != nil {
					code = 1
					if process.ProcessState != nil {
						code = process.ProcessState.ExitCode()
					}
					if code < 0 {
						code = 1
					}
					diagnostic = err.Error()
				}
			}
		}
	}
	return true, report(TaskMessageData{Type: "tool_result", Output: output, Content: diagnostic, Input: map[string]any{"exit_code": code}})
}

func findRemoteSOP(env *execenv.Environment, provider, name string) (string, []string) {
	roots := []string{env.SkillsDir(provider), filepath.Join(env.WorkDir, ".agents", "skills"), filepath.Join(env.WorkDir, ".agents")}
	available := []string{}
	var wrapper string
	for _, root := range roots {
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			dir := filepath.Join(root, entry.Name())
			debug, err := os.Stat(filepath.Join(dir, "DEBUG.md"))
			if err != nil || debug.IsDir() {
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

func (d *Daemon) interceptTaskRemoteSOPs(ctx context.Context, task *Task, state remoteSOPContext) (bool, error) {
	if task.TriggerCommentID == "" {
		return false, nil
	}
	comments := append(append([]CoalescedCommentData{}, task.CoalescedComments...), CoalescedCommentData{ID: task.TriggerCommentID, Content: task.TriggerCommentContent})
	remaining := []CoalescedCommentData{}
	for _, comment := range comments {
		handled, err := d.interceptRemoteSOP(ctx, state, comment.ID, comment.Content)
		if err != nil {
			return false, err
		}
		if !handled {
			remaining = append(remaining, comment)
		}
	}
	if len(remaining) == len(comments) {
		return false, nil
	}
	if len(remaining) == 0 {
		return true, nil
	}
	last := remaining[len(remaining)-1]
	task.TriggerCommentID, task.TriggerCommentContent = last.ID, last.Content
	task.CoalescedComments, task.CoalescedCommentIDs = remaining[:len(remaining)-1], nil
	for _, comment := range task.CoalescedComments {
		task.CoalescedCommentIDs = append(task.CoalescedCommentIDs, comment.ID)
	}
	return false, nil
}
