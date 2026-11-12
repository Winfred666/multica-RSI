package cli

import (
	"context"
	"embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed sop/*.py
var sopEngine embed.FS

var sopName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

// SOP identifies an executable skill without importing its controller.
type SOP struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// SOPSkillRoots returns agent-installed roots from the injected task environment.
func SOPSkillRoots() []string {
	roots := filepath.SplitList(os.Getenv("MULTICA_SOP_SKILL_ROOTS"))
	for _, variable := range []string{"CODEX_HOME", "HERMES_HOME", "CLAUDE_CONFIG_DIR"} {
		if home := os.Getenv(variable); home != "" {
			roots = append(roots, filepath.Join(home, "skills"))
		}
	}
	return roots
}

// DiscoverSOPs reuses the remote resolver's shallow skill-root scan. Repository
// roots follow installed roots so the repository wins collisions by skill name.
func DiscoverSOPs(workdir string, installedRoots []string) ([]SOP, error) {
	workdir, err := filepath.Abs(workdir)
	if err != nil {
		return nil, err
	}
	repo := workdir
	for dir := workdir; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			repo = dir
			break
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	roots := slices.Clone(installedRoots)
	for _, dir := range []string{".agents", ".agents/skills", ".claude/skills", ".codex/skills", ".github/skills", ".agent_context/skills", "skills", "harness/skills"} {
		roots = append(roots, filepath.Join(repo, filepath.FromSlash(dir)))
	}
	byName := map[string]SOP{}
	for _, root := range roots {
		if root == "" {
			continue
		}
		root, err = filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			dir := filepath.Join(root, entry.Name())
			content, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
			if err != nil {
				continue
			}
			parts := strings.SplitN(strings.ReplaceAll(string(content), "\r\n", "\n"), "---\n", 3)
			if len(parts) != 3 || parts[0] != "" {
				continue
			}
			var header struct {
				Name     string `yaml:"name"`
				Metadata struct {
					SOP string `yaml:"sop"`
				} `yaml:"metadata"`
			}
			if yaml.Unmarshal([]byte(parts[1]), &header) != nil || !sopName.MatchString(header.Name) || header.Metadata.SOP != "symbolic" {
				continue
			}
			byName[header.Name] = SOP{Name: header.Name, Path: dir}
		}
	}
	result := make([]SOP, 0, len(byName))
	for _, sop := range byName {
		result = append(result, sop)
	}
	slices.SortFunc(result, func(a, b SOP) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

// RunSOP executes the embedded Python runtime, keeping the caller's cwd and
// providing separate read-only semantics and writable execution directories.
func RunSOP(ctx context.Context, sop SOP, stateDir, workdir string, args, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
	// monitor.py can outlive this invocation while an experiment runs.
	runtimeDir := filepath.Join(stateDir, "engine")
	if err := os.MkdirAll(runtimeDir, 0700); err != nil {
		return err
	}
	files, err := sopEngine.ReadDir("sop")
	if err != nil {
		return err
	}
	for _, file := range files {
		content, err := sopEngine.ReadFile("sop/" + file.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(runtimeDir, file.Name()), content, 0600); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return err
	}
	argv := []string{filepath.Join(runtimeDir, "wrapper.py"), "--sop-dir", sop.Path, "--state-dir", stateDir, "--name", sop.Name, "--"}
	process := exec.CommandContext(ctx, "python3", append(argv, args...)...)
	process.Dir = workdir
	process.Env = env
	process.Stdin, process.Stdout, process.Stderr = stdin, stdout, stderr
	if err := process.Run(); err != nil {
		return fmt.Errorf("run SOP %s: %w", sop.Name, err)
	}
	return nil
}
