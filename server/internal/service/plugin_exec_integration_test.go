//go:build linux || darwin

package service_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/scheduler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

func hostedPluginFixture(t *testing.T) (*service.PluginService, *testutil.Fixture) {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	f.UserID = f.User(t, "Plugin owner", suffix+"@example.com")
	f.WorkspaceID = f.Workspace(t, "Hosted plugins", "hosted-"+suffix)
	f.Member(t, f.WorkspaceID, f.UserID, "owner")
	f.Cleanup(t, `DELETE FROM personal_access_token WHERE user_id = $1`, f.UserID)
	s := service.NewPluginService(db.New(pool), pool)
	s.Secrets, err = secretbox.New(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s.ServerURL = "https://multica.example.com"
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.PluginsV1, featureflag.Rule{Default: true})
	s.FeatureFlags = featureflag.NewService(provider)
	t.Cleanup(func() {
		_ = s.Queries.DeleteWorkspacePluginData(context.Background(), mustParseUUID(t, f.WorkspaceID))
	})
	return s, f
}

func publishHostedPlugin(t *testing.T, s *service.PluginService, f *testutil.Fixture, version, script string) string {
	t.Helper()
	manifest := fmt.Sprintf(`{
		"manifest_version":1,"key":"com.example.hosted","name":"Hosted test",
		"version":%q,"author":{"name":"test"},"scopes":["host:exec"],
		"contributes":{"hooks":[{"key":"check","name":"Check","description":"Check state",
		"triggers":["manual","schedule"],"schedule":{"cron":"*/5 * * * *","timezone":"UTC"},
		"transport":{"type":"exec","entry":"hooks/check.sh"}}]}
	}`, version)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range map[string]string{"multica.plugin.json": manifest, "hooks/check.sh": script} {
		file, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	pkg, err := s.PublishBundle(context.Background(), mustParseUUID(t, f.WorkspaceID), mustParseUUID(t, f.UserID), buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range pkg.Versions {
		if candidate.Version == version {
			return candidate.ID
		}
	}
	t.Fatal("published version missing")
	return ""
}

func installHostedPlugin(t *testing.T, s *service.PluginService, f *testutil.Fixture, version string) db.PluginInstallation {
	t.Helper()
	i, err := s.InstallPlugin(context.Background(), mustParseUUID(t, f.WorkspaceID), mustParseUUID(t, f.UserID), version, []string{"host:exec"})
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func suppliedPAT(t *testing.T, f *testutil.Fixture, userID string) (string, string) {
	t.Helper()
	token, err := auth.GeneratePATToken()
	if err != nil {
		t.Fatal(err)
	}
	id := f.Insert(t, "personal_access_token", testutil.Cols{
		"user_id": userID, "name": "Explicit plugin PAT", "token_hash": auth.HashToken(token), "token_prefix": token[:12],
	})
	return token, id
}

func invokeHostedPlugin(s *service.PluginService, i db.PluginInstallation) (service.HookResult, error) {
	hook, err := service.FindHook(i, "check")
	if err != nil {
		return service.HookResult{}, err
	}
	return s.InvokeHook(context.Background(), service.HookInvocation{
		Installation: i, Hook: hook, Trigger: plugincontract.TriggerManual,
		Actor: service.HookActor{Type: "user", ID: i.InstalledBy},
	}, 1)
}

func TestHostedPluginWorkspaceCredentialLifecycle(t *testing.T) {
	ctx := context.Background()
	s, f := hostedPluginFixture(t)
	wsID := mustParseUUID(t, f.WorkspaceID)
	version := publishHostedPlugin(t, s, f, "1.0.0", `#!/bin/sh
payload=$(cat)
printf '{"input":%s,"token":"%s","workspace":"%s","server":"%s"}' "$payload" "$MULTICA_TOKEN" "$MULTICA_WORKSPACE_ID" "$MULTICA_SERVER_URL"
`)
	i := installHostedPlugin(t, s, f, version)
	status, err := s.PluginExecutionStatus(ctx, wsID)
	if err != nil || status.Enabled || status.Configured || status.RunAs != nil {
		t.Fatal("execution not disabled by default", err)
	}
	if _, err := invokeHostedPlugin(s, i); err == nil {
		t.Fatal("hook ran without an explicitly supplied PAT")
	}
	var count int
	f.QueryRow(t, `SELECT count(*) FROM personal_access_token WHERE user_id = $1`, f.UserID).Scan(&count)
	if count != 0 {
		t.Fatal("install automatically created a PAT")
	}

	alice := f.User(t, "Alice", uuid.NewString()+"@example.com")
	f.Member(t, f.WorkspaceID, alice, "member")
	token, patID := suppliedPAT(t, f, alice)
	status, err = s.SetPluginExecution(ctx, wsID, token)
	if err != nil || !status.Enabled || status.RunAs == nil || status.RunAs.ID != alice || status.RunAs.Name != "Alice" {
		t.Fatal("supplied PAT owner not used", err)
	}
	stored, err := s.Queries.GetWorkspacePluginExecution(ctx, wsID)
	if err != nil || bytes.Contains(stored.Ciphertext, []byte(token)) || stored.PatID != mustParseUUID(t, patID) {
		t.Fatal("credential not encrypted/bound", err)
	}
	result, err := invokeHostedPlugin(s, i)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Input                    map[string]any
		Token, Workspace, Server string
	}
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatal(err)
	}
	if output.Token != "[REDACTED]" || output.Workspace != f.WorkspaceID || output.Server != s.ServerURL || output.Input["trigger"] != "manual" || bytes.Contains(result.Output, []byte(token)) {
		t.Fatal("hook environment/payload leaked or incorrect")
	}

	version2 := publishHostedPlugin(t, s, f, "2.0.0", "#!/bin/sh\nprintf '{\"version\":2}'\n")
	result, err = invokeHostedPlugin(s, i)
	if err != nil || !bytes.Contains(result.Output, []byte(`"input"`)) {
		t.Fatal("publishing changed installed code", err)
	}
	i = installHostedPlugin(t, s, f, version2)
	result, err = invokeHostedPlugin(s, i)
	if err != nil || string(result.Output) != `{"version":2}` {
		t.Fatal("upgrade did not execute pinned code", err)
	}
	status, err = s.PluginExecutionStatus(ctx, wsID)
	if err != nil || status.RunAs == nil || status.RunAs.ID != alice {
		t.Fatal("upgrade changed execution owner", err)
	}
	if _, err := s.SetEnabled(ctx, i, false); err != nil {
		t.Fatal(err)
	}
	if _, err := invokeHostedPlugin(s, i); err == nil {
		t.Fatal("disabled installation executed")
	}
	disabled, err := s.Queries.GetPluginInstallation(ctx, i.ID)
	if err != nil {
		t.Fatal(err)
	}
	i, err = s.SetEnabled(ctx, disabled, true)
	if err != nil {
		t.Fatal(err)
	}
	storedAgain, err := s.Queries.GetWorkspacePluginExecution(ctx, wsID)
	if err != nil || storedAgain.PatID != stored.PatID {
		t.Fatal("lifecycle replaced supplied PAT", err)
	}
	if err := s.DeletePluginExecution(ctx, wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := invokeHostedPlugin(s, i); err == nil {
		t.Fatal("disconnect still allowed execution")
	}
	if _, err := s.Queries.GetPersonalAccessTokenByHash(ctx, auth.HashToken(token)); err != nil {
		t.Fatal("disconnect revoked supplier's PAT", err)
	}
	if _, err := s.SetPluginExecution(ctx, wsID, token); err != nil {
		t.Fatal(err)
	}
	if err := s.Uninstall(ctx, i); err != nil {
		t.Fatal(err)
	}
	if status, err := s.PluginExecutionStatus(ctx, wsID); err != nil || !status.Enabled {
		t.Fatal("uninstall removed shared credential", err)
	}
	if err := s.Queries.DeleteWorkspacePluginData(ctx, wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Queries.GetWorkspacePluginExecution(ctx, wsID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("workspace deletion retained credential", err)
	}
	if _, err := s.Queries.GetPersonalAccessTokenByHash(ctx, auth.HashToken(token)); err != nil {
		t.Fatal("workspace deletion revoked supplier's PAT", err)
	}
}

func TestHostedPluginCredentialIsolationAndRevocation(t *testing.T) {
	ctx := context.Background()
	s, f := hostedPluginFixture(t)
	other, otherFixture := hostedPluginFixture(t)
	workspace := mustParseUUID(t, f.WorkspaceID)
	otherWorkspace := mustParseUUID(t, otherFixture.WorkspaceID)
	token, id := suppliedPAT(t, f, f.UserID)
	if _, err := s.SetPluginExecution(ctx, workspace, token); err != nil {
		t.Fatal(err)
	}
	if status, err := other.PluginExecutionStatus(ctx, otherWorkspace); err != nil || status.Configured {
		t.Fatal("credential crossed workspace boundary", err)
	}
	if _, err := other.SetPluginExecution(ctx, otherWorkspace, token); err == nil {
		t.Fatal("accepted PAT owner outside workspace")
	}
	otherToken, _ := suppliedPAT(t, otherFixture, otherFixture.UserID)
	if _, err := other.SetPluginExecution(ctx, otherWorkspace, otherToken); err != nil {
		t.Fatal(err)
	}
	version := publishHostedPlugin(t, s, f, "1.0.0", "#!/bin/sh\nprintf '{}'\n")
	i := installHostedPlugin(t, s, f, version)
	f.Exec(t, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, f.WorkspaceID, f.UserID)
	if _, err := invokeHostedPlugin(s, i); err == nil {
		t.Fatal("departed owner still executed")
	}
	f.Member(t, f.WorkspaceID, f.UserID, "owner")
	f.Exec(t, `UPDATE personal_access_token SET revoked = TRUE WHERE id = $1`, id)
	if _, err := invokeHostedPlugin(s, i); err == nil {
		t.Fatal("revoked PAT silently replaced")
	}
	if status, err := s.PluginExecutionStatus(ctx, workspace); err != nil || !status.Configured || status.Enabled {
		t.Fatal("revoked PAT reported enabled", err)
	}
	if status, err := other.PluginExecutionStatus(ctx, otherWorkspace); err != nil || !status.Enabled {
		t.Fatal("revocation affected another workspace", err)
	}
	freshToken, _ := suppliedPAT(t, f, f.UserID)
	box := s.Secrets
	s.Secrets = nil
	if _, err := s.SetPluginExecution(ctx, workspace, freshToken); err == nil {
		t.Fatal("accepted credential without encryption")
	}
	s.Secrets = box
	saved, err := s.Queries.GetWorkspacePluginExecution(ctx, workspace)
	if err != nil || saved.PatID != mustParseUUID(t, id) {
		t.Fatal("failed update changed credential", err)
	}
	if _, err := s.SetPluginExecution(ctx, workspace, freshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := invokeHostedPlugin(s, i); err != nil {
		t.Fatal("replacement PAT not used", err)
	}
}

func TestHostedPluginScheduleRetry(t *testing.T) {
	ctx := context.Background()
	s, f := hostedPluginFixture(t)
	token, _ := suppliedPAT(t, f, f.UserID)
	if _, err := s.SetPluginExecution(ctx, mustParseUUID(t, f.WorkspaceID), token); err != nil {
		t.Fatal(err)
	}

	version := publishHostedPlugin(t, s, f, "1.0.0", `#!/bin/sh
payload=$(cat)
case "$payload" in *'"attempt":1'*) exit 1;; esac
test -n "$MULTICA_TOKEN" || exit 2
printf '{}'
`)
	i := installHostedPlugin(t, s, f, version)
	job := scheduler.PluginHookScheduleDispatchJob(s.Queries, s)
	scopes, err := job.Scopes(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Queries.ListPluginHookSchedulesByInstallation(ctx, i.ID)
	if err != nil || len(rows) != 1 {
		t.Fatal("missing schedule", err)
	}
	var scope scheduler.Scope
	for _, candidate := range scopes {
		if strings.HasPrefix(candidate.ID, uuid.UUID(rows[0].ID.Bytes).String()+":") {
			scope = candidate
		}
	}
	if scope.ID == "" {
		t.Fatal("scheduler omitted exec hook")
	}
	in := scheduler.HandlerInput{Job: &job, Scope: scope, PlanTime: time.Now().UTC().Truncate(5 * time.Minute), Attempt: 1}
	if _, err := job.Handler(ctx, in); err == nil {
		t.Fatal("failed executable did not fail scheduler delivery")
	}
	in.Attempt = 2
	result, err := job.Handler(ctx, in)
	if err != nil || result.RowsAffected != 1 {
		t.Fatalf("retry did not execute hook: %+v %v", result, err)
	}
	var attempts, deliveries, failures int
	f.QueryRow(t, `SELECT count(*), count(DISTINCT delivery_id), count(*) FILTER (WHERE status <> 'ok') FROM plugin_invocation WHERE installation_id = $1`, i.ID).Scan(&attempts, &deliveries, &failures)
	if attempts != 2 || deliveries != 1 || failures != 1 {
		t.Fatalf("retry telemetry: attempts=%d deliveries=%d failures=%d", attempts, deliveries, failures)
	}
	if _, err := s.SetEnabled(ctx, i, false); err != nil {
		t.Fatal(err)
	}
	result, err = job.Handler(ctx, in)
	if err != nil || result.RowsAffected != 0 {
		t.Fatal("disabled schedule dispatched", err)
	}
}

func TestHostedPluginCredentialWriteWaitsForWorkspaceDeletion(t *testing.T) {
	ctx := context.Background()
	s, f := hostedPluginFixture(t)
	token, _ := suppliedPAT(t, f, f.UserID)
	tx, err := f.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	workspace := mustParseUUID(t, f.WorkspaceID)
	if _, err := s.Queries.WithTx(tx).LockWorkspaceForDelete(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if _, err := s.SetPluginExecution(deadline, workspace, token); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("credential write did not wait for deletion", err)
	}
	if status, err := s.PluginExecutionStatus(ctx, workspace); err != nil || status.Configured {
		t.Fatal("blocked write leaked credential", err)
	}
}
