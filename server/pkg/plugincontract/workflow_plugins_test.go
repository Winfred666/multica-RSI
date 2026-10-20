package plugincontract

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkflowPluginManifests(t *testing.T) {
	for _, name := range []string{"wakeup-for-sub-issue", "in-progress-ralph"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "..", "plugins", name, ManifestFilename))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := ParseManifest(raw); err != nil {
				t.Fatal(err)
			}
		})
	}
}
