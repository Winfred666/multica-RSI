package execenv

import (
	"strings"
	"testing"
)

const startupContextReferences = `{{.AgentSkills}}
{{.RequestingUserName}} {{.RequestingUserProfileDescription}}
{{.Repos}} {{.ProjectID}} {{.ProjectTitle}} {{.ProjectDescription}} {{.ProjectResources}}
{{.IssueStatuses}} {{.IssueStatusesOmitted}}
{{.OS}} {{.Provider}}`

func TestValidateStartupTemplate(t *testing.T) {
	const source = "{{if .AgentName}}{{.AgentName | markdown}}{{end}} {{$.AgentID}} {{with .AgentInstructions}}{{.}}{{end}} " + startupContextReferences
	for _, tc := range []struct {
		name, source, wantError string
	}{
		{name: "valid conditionals and loops", source: strings.ReplaceAll(source, "{{.AgentSkills}}", "{{range .AgentSkills}}{{.Name}}{{end}}")},
		{name: "parse error", source: source + "{{if .IssueID}}", wantError: "unexpected EOF"},
		{name: "unknown function", source: source + "{{unknown .TaskID}}", wantError: `function "unknown" not defined`},
		{name: "missing skills", source: strings.ReplaceAll(source, "{{.AgentSkills}}", ""), wantError: ".AgentSkills"},
		{name: "literal references do not count", source: strings.ReplaceAll(source, "{{.AgentSkills}}", `.AgentSkills {{/* .AgentSkills */}} {{".AgentSkills"}}`), wantError: ".AgentSkills"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateStartupTemplate(tc.source)
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
		})
	}
}
