package harness

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestEmbeddedKit(t *testing.T) {
	raw, err := Files.ReadFile("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SystemSkills []string `json:"system_skills"`
		Skills       []string `json:"skills"`
		Plugins      []string `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(manifest.Skills, "multica-workspace-manipulation") || slices.Contains(manifest.SystemSkills, "multica-workspace-manipulation") {
		t.Fatal("workspace manipulation must be a default library skill, not a system skill")
	}
	for _, name := range append(manifest.SystemSkills, manifest.Skills...) {
		if _, err := Files.ReadFile("skills/" + name + "/SKILL.md"); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range manifest.Plugins {
		if _, err := Files.ReadFile("plugins/" + name + "/multica.plugin.json"); err != nil {
			t.Fatal(err)
		}
	}
}
