//go:build linux || darwin

package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPluginExecutableInputAndIsolatedEnvironment(t *testing.T) {
	t.Setenv("MULTICA_TOKEN", "parent-token-must-not-leak")
	t.Setenv("DATABASE_URL", "parent-database-must-not-leak")
	script := []byte("#!/bin/sh\n[ -z \"$MULTICA_TOKEN$DATABASE_URL\" ] || exit 1\n[ \"$MULTICA_WORKSPACE_ID\" = fixture-workspace ] || exit 2\ncat\n")
	input := []byte(`{"config":{"limit":3},"delivery_id":"delivery-1","attempt":2}`)
	output, err := runPluginExecutable(context.Background(), script, input, []string{"MULTICA_WORKSPACE_ID=fixture-workspace"})
	if err != nil || string(output) != string(input) {
		t.Fatalf("output=%s err=%v", output, err)
	}

	output, err = runPluginExecutable(context.Background(), []byte("#!/bin/sh\nprintf '{\"dir\":\"%s\"}' \"$PWD\"\n"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var result struct{ Dir string }
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(result.Dir); !os.IsNotExist(err) {
		t.Fatalf("working directory was not removed: %v", err)
	}
}

func TestPluginExecutableFailureReporting(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{
		{"empty", "#!/bin/sh\nexit 0\n", ""},
		{"non JSON", "#!/bin/sh\nprintf 'not JSON'\n", "non-JSON"},
		{"exit failure", "#!/bin/sh\necho secret-on-stderr >&2\nexit 7\n", "failed"},
		{"missing interpreter", "#!/not/a/real/interpreter\n", "start plugin executable"},
		{"large output", "#!/bin/sh\nhead -c 1048577 /dev/zero\n", "exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			output, err := runPluginExecutable(ctx, []byte(tc.script), nil, nil)
			if tc.want == "" {
				if err != nil || len(output) != 0 {
					t.Fatalf("output=%q err=%v", output, err)
				}
				return
			}
			if err == nil || !strings.Contains(redactHookError(err), tc.want) || strings.Contains(err.Error(), "secret-on-stderr") {
				t.Fatalf("error=%v, want %q without stderr", err, tc.want)
			}
		})
	}
}

func TestPluginExecutableTerminatesChildren(t *testing.T) {
	for _, wait := range []bool{true, false} {
		t.Run(map[bool]string{true: "timeout", false: "parent exits"}[wait], func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "survived")
			script := "#!/bin/sh\nread -r marker\n(sleep 0.3; echo survived > \"$marker\") >/dev/null 2>&1 &\n"
			if wait {
				script += "wait\n"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err := runPluginExecutable(ctx, []byte(script), []byte(marker+"\n"), nil)
			if wait && (!errors.Is(err, context.DeadlineExceeded) || hookFailureStatus(err) != "timeout") {
				t.Fatalf("want recorded timeout, got %v", err)
			}
			if !wait && err != nil {
				t.Fatal(err)
			}
			time.Sleep(350 * time.Millisecond)
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("background child survived invocation: %v", err)
			}
		})
	}
}
