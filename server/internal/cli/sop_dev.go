package cli

import (
	"context"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed sopdev/*
var sopDeveloperKit embed.FS

func SOPDeveloperDocs() string {
	guide, _ := sopDeveloperKit.ReadFile("sopdev/DEVELOPMENT.md")
	return string(guide)
}

func InitSOP(directory string, featureRich bool) (SOP, error) {
	dir, err := filepath.Abs(directory)
	if err != nil {
		return SOP{}, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return SOP{}, err
	}
	if len(entries) != 0 {
		return SOP{}, fmt.Errorf("target directory must be empty: %s", dir)
	}
	name := strings.Trim(regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(strings.ToLower(filepath.Base(dir)), "-"), "-")
	if name == "" {
		return SOP{}, fmt.Errorf("directory name must contain letters or numbers")
	}
	controller := "minimal.py"
	if featureRich {
		controller = "feature-rich.py"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return SOP{}, err
	}
	for target, source := range map[string]string{
		"SKILL.md": "SKILL.md", "controller.py": controller, "DEVELOPMENT.md": "DEVELOPMENT.md",
	} {
		content, err := sopDeveloperKit.ReadFile("sopdev/" + source)
		if err != nil {
			return SOP{}, err
		}
		if err := os.WriteFile(filepath.Join(dir, target), []byte(strings.ReplaceAll(string(content), "{{name}}", name)), 0644); err != nil {
			return SOP{}, err
		}
	}
	return SOP{Name: name, Path: dir}, nil
}

func ValidateSOP(ctx context.Context, directory string, stdout, stderr io.Writer) error {
	dir, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	sop, err := readSOP(dir)
	if err != nil {
		return err
	}
	stateDir, err := os.MkdirTemp("", "multica-sop-validate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stateDir)
	workdir, err := os.Getwd()
	if err != nil {
		return err
	}
	return RunSOP(ctx, sop, stateDir, workdir, []string{"--validate"}, os.Environ(), nil, stdout, stderr)
}
