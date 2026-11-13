package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiscoverSOPs(t *testing.T) {
	root := t.TempDir()
	repo, installed := filepath.Join(root, "repo"), filepath.Join(root, "installed")
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(repo, ".git"), "gitdir: worktree")
	add := func(dir, name string) {
		write(filepath.Join(dir, "SKILL.md"), "---\nname: "+name+"\nmetadata: {sop: symbolic}\n---\n")
		write(filepath.Join(dir, "controller.py"), "raise RuntimeError('discovery must not import controllers')")
	}
	add(filepath.Join(installed, "different-folder"), "installed-only")
	add(filepath.Join(installed, "same"), "shared")
	repositoryOnly := filepath.Join(repo, ".agents", "skills", "repo")
	add(repositoryOnly, "repository-only")
	preferred := filepath.Join(repo, ".claude", "skills", "same-name")
	add(preferred, "shared")
	write(filepath.Join(installed, "text-only", "SKILL.md"), "---\nname: text-only\n---\n")
	write(filepath.Join(installed, "text-only", "wrapper.py"), "# Not executable metadata")
	nested := filepath.Join(repo, "server", "cmd")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverSOPs(nested, []string{installed})
	if err != nil {
		t.Fatal(err)
	}
	want := []SOP{{"installed-only", filepath.Join(installed, "different-folder")}, {"repository-only", repositoryOnly}, {"shared", preferred}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestRunSOPUsesEmbeddedEngine(t *testing.T) {
	root := t.TempDir()
	skill, state, work := filepath.Join(root, "skill"), filepath.Join(root, "state"), filepath.Join(root, "work")
	for _, dir := range []string{skill, work} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"SKILL.md": "---\nname: inspect\nmetadata: {sop: symbolic}\n---\n",
		"controller.py": `import json, os, sys
from sop import State, Workflow
assert not sys.dont_write_bytecode
print(json.dumps(sys.argv[sys.argv.index('--') + 1:]))
print(os.getcwd())
print(__file__)
print('controller stderr', file=sys.stderr)
raise SystemExit(7)
`,
		// A skill's stale engine copies must not replace the CLI's shared API.
		"sop.py": "raise RuntimeError('do not load copied engine')",
	} {
		if err := os.WriteFile(filepath.Join(skill, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PYTHONDONTWRITEBYTECODE", "")
	t.Setenv("MULTICA_ISSUE_ID", "fixture")
	t.Setenv("MULTICA_WORKSPACE_ID", "fixture")
	var out, stderr bytes.Buffer
	err := RunSOP(context.Background(), SOP{"inspect", skill}, state, work,
		[]string{"inspect", "--help", "two words", "", "$(literal)", "--", "-x"}, os.Environ(), nil, &out, &stderr)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("exit = %v; output = %s%s", err, &out, &stderr)
	}
	want := "[\"inspect\", \"--help\", \"two words\", \"\", \"$(literal)\", \"--\", \"-x\"]\n" + work + "\n" + filepath.Join(skill, "controller.py") + "\n"
	if out.String() != want || stderr.String() != "controller stderr\n" {
		t.Fatalf("stdout = %q; stderr = %q", &out, &stderr)
	}
	if _, err := os.Stat(state); err != nil {
		t.Fatal(err)
	}
}

func TestSOPDeveloperKit(t *testing.T) {
	t.Setenv("MULTICA_ISSUE_ID", "")
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")
	for _, rich := range []bool{false, true} {
		t.Run(map[bool]string{false: "minimal", true: "feature-rich"}[rich], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "My SOP")
			if rich {
				if err := os.Mkdir(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			sop, err := InitSOP(dir, rich)
			if err != nil || sop.Name != "my-sop" {
				t.Fatalf("init = %+v, %v", sop, err)
			}
			guide, err := os.ReadFile(filepath.Join(dir, "DEVELOPMENT.md"))
			if err != nil || string(guide) != SOPDeveloperDocs() {
				t.Fatalf("offline guide differs: %v", err)
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 3 {
				t.Fatalf("expected only skill, controller and guide: %v, %v", files, err)
			}
			if _, err := InitSOP(dir, rich); err == nil {
				t.Fatal("init overwrote a populated directory")
			}
			var out, stderr bytes.Buffer
			err = ValidateSOP(context.Background(), dir, &out, &stderr)
			if rich {
				// Report the existing controller for explicit migration, never rewrite it.
				if err == nil || !strings.Contains(out.String(), "cycle: route -> review -> inspect -> route") {
					t.Fatalf("legacy cycle not reported: %v, %s%s", err, &out, &stderr)
				}
			} else if err != nil || out.String() != "{\"status\": \"valid\"}\n" {
				t.Fatalf("validate: %v, %s%s", err, &out, &stderr)
			}
			if err := os.WriteFile(filepath.Join(dir, "controller.py"), []byte("workflow = None"), 0644); err != nil {
				t.Fatal(err)
			}
			out.Reset()
			if err := ValidateSOP(context.Background(), dir, &out, &stderr); err == nil {
				t.Fatal("validation accepted a missing Workflow")
			}
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: text-only\n---\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := ValidateSOP(context.Background(), dir, &out, &stderr); err == nil {
				t.Fatal("validation accepted missing symbolic metadata")
			}
		})
	}
}
