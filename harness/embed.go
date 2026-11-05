// Package harness ships the workspace startup kit with the server.
package harness

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed startup.gotmpl manifest.json skills plugins/runtime.py plugins/*/hook.py plugins/*/multica.plugin.json
var Files embed.FS

// Install runs the same self-contained installer shipped as a workspace skill.
func Install(ctx context.Context, baseURL, token, workspaceID string, keepContext, plugins bool) error {
	directory, err := os.MkdirTemp("", "multica-harness-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	if err := fs.WalkDir(Files, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		target := filepath.Join(directory, filepath.FromSlash(path))
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := Files.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	}); err != nil {
		return err
	}
	args := []string{filepath.Join(directory, "skills", "harness-backup-restore", "scripts", "harness.py"), "install", "--kit", directory}
	if keepContext {
		args = append(args, "--keep-context")
	}
	if !plugins {
		args = append(args, "--without-plugins")
	}
	command := exec.CommandContext(ctx, "python3", args...)
	command.Env = append(os.Environ(), "MULTICA_SERVER_URL="+baseURL,
		"MULTICA_WORKSPACE_ID="+workspaceID, "MULTICA_TOKEN="+token,
		"PYTHONDONTWRITEBYTECODE=1")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("install startup kit: %w: %s", err, output)
	}
	return nil
}
