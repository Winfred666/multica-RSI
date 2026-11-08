package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

var SOPNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,59}$`)

type SOPRegistration struct {
	ServerURL   string   `json:"server_url"`
	WorkspaceID string   `json:"workspace_id"`
	IssueID     string   `json:"issue_id"`
	Name        string   `json:"name"`
	Executable  string   `json:"executable"`
	Args        []string `json:"args,omitempty"`
	Inputs      []string `json:"inputs"`
}

func SOPStateDir(profile string) (string, error) {
	dir, err := ProfileDir(profile)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sop"), nil
}

func ReadSOPRegistry(dir string) ([]SOPRegistration, error) {
	var entries []SOPRegistration
	data, err := os.ReadFile(filepath.Join(dir, "registry.json"))
	if errors.Is(err, os.ErrNotExist) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &entries)
	return entries, err
}

func WriteSOPJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".sop-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
