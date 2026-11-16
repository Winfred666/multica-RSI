//go:build agentintegration

package execenv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Opt-in real loader coverage: task additions must preserve native MCP servers
// and sibling settings. Gated by agentintegration and MULTICA_RUN_REAL_AGENT_SMOKE.
// The fixture chooses a sibling accepted by the installed OpenClaw schema.
type mcpSibling struct {
	key      string
	fragment string
	want     any
}

var mcpSiblingCandidates = []mcpSibling{
	{key: "sessionIdleTtlMs", fragment: `"sessionIdleTtlMs": 300000`, want: float64(300000)},
	{key: "apps", fragment: `"apps": {}`, want: map[string]any{}},
}

func realOpenclawConfig(t *testing.T) (bin, activeConfig string, sibling mcpSibling) {
	t.Helper()
	bin = realOpenclawBin(t)

	home := t.TempDir()
	stateDir := filepath.Join(home, ".openclaw")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("create isolated OpenClaw state: %v", err)
	}
	activeConfig = filepath.Join(stateDir, "openclaw.json")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("OPENCLAW_HOME", home)
	t.Setenv("OPENCLAW_STATE_DIR", stateDir)
	t.Setenv("OPENCLAW_CONFIG_PATH", activeConfig)

	for _, candidate := range mcpSiblingCandidates {
		writeRealOpenclawUserConfig(t, activeConfig, candidate)
		if realOpenclawConfigValid(t, bin) {
			return bin, activeConfig, candidate
		}
		t.Logf("channel rejects mcp.%s; trying the next sibling shape", candidate.key)
	}
	t.Fatalf("no candidate mcp sibling is accepted by this channel; the fixture needs a shape from its schema")
	return "", "", mcpSibling{}
}

func writeRealOpenclawUserConfig(t *testing.T, path string, sibling mcpSibling) {
	t.Helper()
	config := fmt.Sprintf(`{
		"gateway": {"mode": "local"},
		"logging": {"level": "debug"},
		"mcp": {
			%s,
			"servers": {
				"user-only": {"command": "user-only"},
				"shared": {"command": "user-shared"}
			}
		}
	}`, sibling.fragment)
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write isolated OpenClaw config: %v", err)
	}
}

// realOpenclawConfigValid reports whether the CLI accepts the config it is
// currently pointed at. `config validate --json` carries `valid` on every branch
// since 2026.5.5 and exits non-zero for an invalid config, so the exit status is
// deliberately ignored in favour of the payload.
func realOpenclawConfigValid(t *testing.T, bin string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), openclawCLITimeout)
	defer cancel()
	out, _ := openclawExec(ctx, bin, "config", "validate", "--json")
	var payload struct {
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse `config validate --json` output %q: %v", out, err)
	}
	return payload.Valid
}

func realOpenclawConfigGetJSON(t *testing.T, bin, keyPath string) any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), openclawCLITimeout)
	defer cancel()
	out, err := openclawExec(ctx, bin, "config", "get", keyPath, "--json")
	if err != nil {
		t.Fatalf("resolve %s through the real CLI: %v", keyPath, annotateOpenclawJSONError(err, out))
	}
	var value any
	if err := json.Unmarshal([]byte(out), &value); err != nil {
		t.Fatalf("parse resolved %s JSON %q: %v", keyPath, out, err)
	}
	return value
}

func TestPrepareOpenclawConfigRealCLI(t *testing.T) {
	bin, activeConfig, sibling := realOpenclawConfig(t)

	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
		OpenclawBin: bin,
		McpConfig: json.RawMessage(`{"mcpServers":{
			"managed-only":{"command":"managed-only"},
			"shared":{"command":"managed-shared"}
		}}`),
	})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig against the real CLI: %v", err)
	}

	// The chain we generated, before asking the loader what it makes of it.
	wrapper := mustReadJSON(t, result.ConfigPath)
	include, ok := wrapper["$include"].([]any)
	if !ok || len(include) != 1 || include[0] != activeConfig {
		t.Fatalf("wrapper $include = %#v, want [%q]", wrapper["$include"], activeConfig)
	}
	if result.IncludeRoot != filepath.Dir(activeConfig) {
		t.Fatalf("include root = %q, want %q", result.IncludeRoot, filepath.Dir(activeConfig))
	}

	t.Setenv("OPENCLAW_CONFIG_PATH", result.ConfigPath)
	t.Setenv("OPENCLAW_INCLUDE_ROOTS", result.IncludeRoot)

	if !realOpenclawConfigValid(t, bin) {
		t.Fatalf("the generated config does not validate")
	}

	// A field that exists only in the live user config, so seeing it proves the
	// wrapper's include followed through to the user's file rather than merely
	// leaving its own managed block intact.
	if level := realOpenclawConfigGetJSON(t, bin, "logging.level"); level != "debug" {
		t.Fatalf("resolved logging.level = %#v, want debug through the include", level)
	}

	resolvedMcp, ok := realOpenclawConfigGetJSON(t, bin, "mcp").(map[string]any)
	if !ok {
		t.Fatalf("resolved mcp is not an object")
	}

	wantServers := map[string]any{
		"user-only":    map[string]any{"command": "user-only"},
		"managed-only": map[string]any{"command": "managed-only"},
		"shared":       map[string]any{"command": "managed-shared"},
	}
	servers, ok := resolvedMcp["servers"].(map[string]any)
	if !ok || !reflect.DeepEqual(servers, wantServers) {
		t.Fatalf("resolved mcp.servers = %#v, want exactly %#v", resolvedMcp["servers"], wantServers)
	}

	got, present := resolvedMcp[sibling.key]
	if !present {
		t.Fatalf("resolved mcp lost the user's %s: %#v",
			sibling.key, resolvedMcp)
	}
	if !reflect.DeepEqual(got, sibling.want) {
		t.Fatalf("resolved mcp.%s = %#v, want the user's own %#v", sibling.key, got, sibling.want)
	}
}
