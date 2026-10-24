//go:build linux || darwin

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

func (s *PluginService) callHookExecutable(ctx context.Context, invocation HookInvocation) (json.RawMessage, error) {
	installation, err := s.Queries.GetWorkspacePluginInstallation(ctx, db.GetWorkspacePluginInstallationParams{
		WorkspaceID: invocation.Installation.WorkspaceID,
		ID:          invocation.Installation.ID,
	})
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "load executable installation", Err: err}
	}
	if !installation.Enabled || installation.PackageVersionID != invocation.Installation.PackageVersionID {
		return nil, pluginErrf(PluginErrorForbidden, "plugin installation was disabled or changed")
	}
	if !hasScope(decodeScopes(installation.GrantedScopes), plugincontract.ScopeHostExec) {
		return nil, pluginErrf(PluginErrorForbidden, "hosted execution requires the host:exec grant")
	}
	hook, err := FindHook(installation, invocation.Hook.Key)
	if err != nil {
		return nil, err
	}
	if hook.Transport.Type != plugincontract.TransportExec || !HookAllowsTrigger(hook, invocation.Trigger) {
		return nil, pluginErrf(PluginErrorForbidden, "this hook does not allow hosted execution for this trigger")
	}
	token, err := s.executionToken(ctx, installation)
	if err != nil {
		return nil, err
	}
	file, err := s.Queries.GetPluginPackageFile(ctx, db.GetPluginPackageFileParams{
		VersionID: installation.PackageVersionID,
		Path:      hook.Transport.Entry,
	})
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "load packaged executable", Err: err}
	}
	invocation.Installation = installation
	body, err := s.buildHookBody(ctx, invocation)
	if err != nil {
		return nil, err
	}
	if s.Callbacks != nil && body.CallbackToken != "" {
		defer s.Callbacks.Revoke(body.CallbackToken)
	}
	input, err := json.Marshal(body)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorInvalid, Message: "encode hook request", Err: err}
	}
	timeout := hookDefaultTimeout
	if hook.TimeoutMs > 0 {
		timeout = time.Duration(hook.TimeoutMs) * time.Millisecond
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := runPluginExecutable(callCtx, file.Content, input, []string{
		"MULTICA_TOKEN=" + token,
		"MULTICA_WORKSPACE_ID=" + uuidString(installation.WorkspaceID),
		"MULTICA_SERVER_URL=" + s.ServerURL,
	})
	// A script accidentally echoing its environment must not return its PAT to a surface.
	output = bytes.ReplaceAll(output, []byte(token), []byte("[REDACTED]"))
	return output, err
}

func runPluginExecutable(ctx context.Context, executable, input []byte, environment []string) (json.RawMessage, error) {
	dir, err := os.MkdirTemp("", "multica-plugin-*")
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "prepare plugin working directory", Err: err}
	}
	defer os.RemoveAll(dir)
	entry := filepath.Join(dir, "hook")
	if err := os.WriteFile(entry, executable, 0o700); err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "prepare plugin executable", Err: err}
	}
	cmd := exec.CommandContext(ctx, entry)
	cmd.Dir = dir
	// Do not inherit server secrets or a developer's CLI credentials.
	cmd.Env = append([]string{"PATH=/app:/usr/local/bin:/usr/bin:/bin", "HOME=" + dir, "TMPDIR=" + dir}, environment...)
	cmd.Stdin = bytes.NewReader(input)
	var output hookExecOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "start plugin executable (check its shebang and host dependencies)", Err: err}
	}
	// Clean up background children even when the script exits successfully.
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }()
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "plugin executable cancelled or timed out", Err: ctx.Err()}
	}
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "plugin executable failed", Err: err}
	}
	if output.exceeded {
		return nil, pluginErrf(PluginErrorInvalid, "plugin executable output exceeds %d bytes", hookMaxResponseBytes)
	}
	payload := bytes.TrimSpace(output.buffer.Bytes())
	if len(payload) == 0 {
		return nil, nil
	}
	if !json.Valid(payload) {
		return nil, pluginErrf(PluginErrorInvalid, "plugin executable returned a non-JSON body")
	}
	return json.RawMessage(payload), nil
}

type hookExecOutput struct {
	buffer   bytes.Buffer
	exceeded bool
}

func (b *hookExecOutput) Write(p []byte) (int, error) {
	n := min(len(p), hookMaxResponseBytes-b.buffer.Len())
	_, _ = b.buffer.Write(p[:n])
	b.exceeded = b.exceeded || n < len(p)
	return len(p), nil
}
