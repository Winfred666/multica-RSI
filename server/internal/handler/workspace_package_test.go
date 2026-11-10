package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

func TestWorkspacePackageSelectiveRoundTrip(t *testing.T) {
	p, err := defaultWorkspacePackage()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWorkspacePackage(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Projects) != 0 || len(p.Issues) != 0 || len(p.Agents) != 0 {
		t.Fatal("startup kit contains work data")
	}
	p.Projects = []PackageProject{{ID: "project", Title: "Portable project", Status: "planned", Priority: "high", Resources: []PackageProjectResource{{ResourceType: "github_repo", ResourceRef: json.RawMessage(`{"url":"  https://github.com/example/portable.git  ","ref":" main "}`)}}}}
	stage := int32(1)
	p.Issues = []PackageIssue{{Stage: &stage, ID: "child", Title: "Child", Status: "backlog", Priority: "none", ProjectID: "project", ParentIssueID: "parent", AgentID: "agent", Metadata: json.RawMessage(`{"git_branch":"feat/portable"}`)}, {ID: "parent", Title: "Parent", Status: "todo", Priority: "high", ProjectID: "project"}}
	p.Agents = []PackageAgent{{ID: "agent", Name: "Portable agent", RuntimeMode: "local", Instructions: "Keep working", Skills: []PackageBinding{{ID: p.Skills[0].ID, Enabled: false}}, MCPs: []PackageBinding{{ID: "mcp", Enabled: false}}}}
	p.MCPs = []PackageMCP{{ID: "mcp", Name: "portable-mcp", Config: json.RawMessage(`{"command":"example","args":[]}`)}}
	ws := dbfx.Insert(t, "workspace", testutil.Cols{"name": "Package destination", "slug": "handler-package-destination"})
	dbfx.Insert(t, "member", testutil.Cols{"workspace_id": ws, "user_id": testUserID, "role": "owner"})
	cleanupWorkspaceHarness(t, ws)
	ctx := context.Background()
	// Roll back the transaction after export, leaving no imported rows behind.
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	imported, err := testHandler.importWorkspacePackage(ctx, tx, parseUUID(ws), parseUUID(testUserID), p)
	if err != nil {
		t.Fatal(err)
	}
	writer := *testHandler
	writer.Queries = testHandler.Queries.WithTx(tx)
	selection := PackageSelection{SystemPrompt: true}
	for _, id := range imported.Projects {
		selection.Projects = append(selection.Projects, id)
	}
	for _, id := range imported.Issues {
		selection.Issues = append(selection.Issues, id)
	}
	for _, id := range imported.Skills {
		selection.Skills = append(selection.Skills, id)
	}
	for _, id := range imported.Agents {
		selection.Agents = append(selection.Agents, id)
	}
	for _, id := range imported.MCPs {
		selection.MCPs = append(selection.MCPs, id)
	}
	for _, id := range imported.Plugins {
		selection.Plugins = append(selection.Plugins, id)
	}
	got, err := writer.exportWorkspacePackage(ctx, parseUUID(ws), selection)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWorkspacePackage(got); err != nil {
		t.Fatal(err)
	}
	if len(got.Plugins) != len(p.Plugins) {
		t.Fatal("plugin bundles were not exported")
	}
	if len(got.Projects) != 1 || len(got.Issues) != 2 || len(got.Agents) != 1 || len(got.MCPs) != 1 || len(got.Skills) != len(p.Skills) {
		t.Fatalf("round-trip resource counts: %+v", imported)
	}
	a, err := writer.Queries.GetAgent(ctx, parseUUID(imported.Agents["agent"]))
	if err != nil {
		t.Fatal(err)
	}
	if a.RuntimeID.Valid || a.Status != "offline" || a.PermissionMode != "private" || string(a.RuntimeConfig) != "{}" {
		t.Fatalf("imported agent retained a runtime: %+v", a)
	}
	child, err := writer.Queries.GetIssue(ctx, parseUUID(imported.Issues["child"]))
	if err != nil {
		t.Fatal(err)
	}
	if uuidToString(child.ParentIssueID) != imported.Issues["parent"] || uuidToString(child.ProjectID) != imported.Projects["project"] || uuidToString(child.AssigneeID) != imported.Agents["agent"] {
		t.Fatal("issue references were not remapped")
	}
	var resourceRef githubRepoRef
	if err := json.Unmarshal(got.Projects[0].Resources[0].ResourceRef, &resourceRef); err != nil || resourceRef.URL != "https://github.com/example/portable.git" || resourceRef.Ref != "main" {
		t.Fatal("project resource was not normalized")
	}

	statusJSON, _ := json.Marshal(got.IssueStatuses)
	if strings.Contains(string(statusJSON), "workspace_id") || strings.Contains(string(statusJSON), "created_at") {
		t.Fatal("status identities leaked into the package")
	}
	if len(got.Agents[0].MCPs) != 1 || got.Agents[0].MCPs[0].Enabled {
		t.Fatal("disabled MCP binding changed")
	}
	// Atomic selectors reuse the importer and do no per-reference reads.
	recorder := &packageReadRecorder{DBTX: tx}
	subsetWriter := writer
	subsetWriter.Queries = db.New(recorder)
	issueSelection := PackageSelection{Issues: []string{strings.ToUpper(imported.Issues["child"])}}
	issueOnly, err := subsetWriter.exportWorkspacePackage(ctx, parseUUID(ws), issueSelection)
	if err != nil || len(recorder.calls) != 2 || len(issueOnly.Issues) != 1 || len(issueOnly.Projects) != 0 {
		t.Fatalf("issue-only export: %v, reads=%d, package=%+v", err, len(recorder.calls), issueOnly)
	}
	detached := issueOnly.Issues[0]
	if detached.ParentIssueID != "" || detached.ProjectID != "" || detached.AgentID != "" || detached.Stage != nil {
		t.Fatal("issue-only export retained external references")
	}
	if _, err := writer.importWorkspacePackage(ctx, tx, parseUUID(ws), parseUUID(testUserID), issueOnly); err != nil {
		t.Fatalf("detached package failed to import: %v", err)
	}
	recorder.calls = nil
	projectOnly, err := subsetWriter.exportWorkspacePackage(ctx, parseUUID(ws), PackageSelection{Projects: selection.Projects})
	if err != nil || len(recorder.calls) != 2 || len(projectOnly.Projects) != 1 || len(projectOnly.Issues) != 0 || len(projectOnly.IssueStatuses) != 0 {
		t.Fatalf("project-only export: %v, reads=%d", err, len(recorder.calls))
	}
	recorder.calls = nil
	if empty, err := subsetWriter.exportWorkspacePackage(ctx, parseUUID(ws), PackageSelection{}); err != nil || len(recorder.calls) != 0 || len(empty.Projects)+len(empty.Issues) != 0 {
		t.Fatal("empty selection read or exported work data")
	}
	for _, selection := range []PackageSelection{{Projects: []string{"unknown"}}, {Issues: []string{"11111111-1111-4111-8111-111111111111"}}} {
		if _, err := subsetWriter.exportWorkspacePackage(ctx, parseUUID(ws), selection); !errors.Is(err, errWorkspacePackageInvalid) {
			t.Fatalf("invalid selector accepted: %v", err)
		}
	}
	for _, issue := range got.Issues {
		if issue.ID == imported.Issues["child"] && (issue.ParentIssueID == "" || issue.ProjectID == "" || issue.Stage == nil) {
			t.Fatal("subset export changed the full package")
		}
	}
	// Reimport exported bundles over the already-installed default versions.
	pluginSelection := PackageSelection{}
	for _, plugin := range got.Plugins {
		pluginSelection.Plugins = append(pluginSelection.Plugins, plugin.ID)
	}
	exportedPlugins, err := selectedPackage(got, &pluginSelection)
	if err != nil {
		t.Fatal(err)
	}
	pluginService := *testHandler.PluginService
	pluginService.Queries = writer.Queries
	before, err := pluginService.ListPackages(ctx, parseUUID(ws))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testHandler.importWorkspacePackage(ctx, tx, parseUUID(ws), parseUUID(testUserID), exportedPlugins); err != nil {
		t.Fatal(err)
	}
	after, err := pluginService.ListPackages(ctx, parseUUID(ws))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("reimport changed published version IDs, digests or counts")
	}
	// Same-version file changes still conflict after the JSONB comparison.
	changed := exportedPlugins
	changed.Plugins = append([]PackagePlugin(nil), exportedPlugins.Plugins[:1]...)
	bundle, err := plugincontract.ParseBundle(changed.Plugins[0].Bundle)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"multica.plugin.json": bundle.Canonical}
	for i, file := range bundle.Files {
		files[file.Path] = file.Content
		if i == 0 {
			files[file.Path] = append(append([]byte{}, file.Content...), '\n')
		}
	}
	changed.Plugins[0].Bundle, err = packageZip(files)
	if err != nil {
		t.Fatal(err)
	}
	_, err = testHandler.importWorkspacePackage(ctx, tx, parseUUID(ws), parseUUID(testUserID), changed)
	var conflict *service.PluginError
	if !errors.As(err, &conflict) || conflict.Kind != service.PluginErrorConflict {
		t.Fatalf("expected immutable-version conflict, got %v", err)
	}
	selected, err := selectedPackage(got, &PackageSelection{Skills: []string{got.Skills[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Skills) != 1 || len(selected.Agents) != 0 || len(selected.Issues) != 0 || selected.SystemPrompt != nil {
		t.Fatal("selection imported excluded resources")
	}
	if _, err := selectedPackage(got, &PackageSelection{Skills: []string{"unknown"}}); err == nil {
		t.Fatal("accepted an unknown selection")
	}
	// Isolate path validation from the plugin feature gate.
	p.Plugins = nil
	// The public endpoint rejects invalid supporting paths before any write.
	p.Skills[0].Files = []CreateSkillFileRequest{{Path: "../escape", Content: "bad"}}
	request := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+ws+"/import", ImportWorkspacePackageRequest{Package: p}), "id", ws)
	testutil.Call(t, testHandler.ImportWorkspacePackage, request).Want(http.StatusBadRequest)
	for _, id := range []string{"unknown", "11111111-1111-4111-8111-111111111111"} {
		req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+ws+"/export", PackageSelection{Skills: []string{id}}), "id", ws)
		testutil.Call(t, testHandler.ExportWorkspacePackage, req).Want(http.StatusBadRequest)
	}
}

func TestWorkspacePackageRejectsNonPortableValues(t *testing.T) {
	for _, tc := range []struct {
		name    string
		issue   *PackageIssue
		project *PackageProject
	}{
		{name: "foreign property definition", issue: &PackageIssue{ID: "issue", Title: "Imported property", Status: "backlog", Priority: "none", Properties: json.RawMessage(`{"11111111-1111-4111-8111-111111111111":"source value"}`)}},
		{name: "missing project daemon", project: &PackageProject{ID: "project", Title: "Imported checkout", Status: "planned", Priority: "none", Resources: []PackageProjectResource{{ResourceType: "local_directory", ResourceRef: json.RawMessage(`{"local_path":"/tmp/review-project","daemon_id":"missing-daemon","execution_mode":"worktree"}`)}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			p := emptyWorkspacePackage()
			p.Skills = []PackageSkill{{ID: "skill", Name: "must-not-write-before-validation", Content: "valid"}}
			if tc.issue != nil {
				p.Issues = []PackageIssue{*tc.issue}
			}
			if tc.project != nil {
				p.Projects = []PackageProject{*tc.project}
			}
			if _, err := testHandler.importWorkspacePackage(ctx, tx, parseUUID(testWorkspaceID), parseUUID(testUserID), p); !errors.Is(err, errWorkspacePackageInvalid) {
				t.Fatalf("wanted rejection, got %v", err)
			}
			_, err = testHandler.Queries.WithTx(tx).GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{WorkspaceID: parseUUID(testWorkspaceID), Name: p.Skills[0].Name})
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatal("invalid package wrote resources before validation")
			}
		})
	}
}

func TestWorkspacePackageDestinationCollisions(t *testing.T) {
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := testHandler.Queries.WithTx(tx)
	ws, user := parseUUID(testWorkspaceID), parseUUID(testUserID)
	existing, err := createSkillWithFilesInTx(ctx, q, skillCreateInput{WorkspaceID: ws, CreatorID: user, Name: "package-system-collision", Content: "destination instructions", IsSystemSkill: true})
	if err != nil {
		t.Fatal(err)
	}
	p := emptyWorkspacePackage()
	p.Skills = []PackageSkill{{ID: "incoming", Name: "package-system-collision", Content: "incoming instructions"}}
	if _, err := testHandler.importWorkspacePackage(ctx, tx, ws, user, p); !errors.Is(err, errWorkspacePackageConflict) {
		t.Fatalf("system classification conflict: %v", err)
	}
	got, err := q.GetSkillInWorkspace(ctx, db.GetSkillInWorkspaceParams{ID: parseUUID(existing.ID), WorkspaceID: ws})
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsSystemSkill || got.Content != "destination instructions" {
		t.Fatal("system skill was replaced or demoted")
	}
	p.Skills[0].IsSystemSkill = true
	if _, err := testHandler.importWorkspacePackage(ctx, tx, ws, user, p); err != nil {
		t.Fatal(err)
	}
	got, err = q.GetSkillInWorkspace(ctx, db.GetSkillInWorkspaceParams{ID: parseUUID(existing.ID), WorkspaceID: ws})
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsSystemSkill || got.Content != "incoming instructions" {
		t.Fatal("system update failed")
	}
	p.Skills = nil
	mcp, err := q.CreateWorkspaceMcpServer(ctx, db.CreateWorkspaceMcpServerParams{WorkspaceID: ws, Name: "package-mcp-collision", Config: []byte(`{"command":"old"}`), CreatedBy: user})
	if err != nil {
		t.Fatal(err)
	}
	p.MCPs = []PackageMCP{{ID: "mcp", Name: mcp.Name, Config: json.RawMessage(`{"command":"new"}`)}}
	if _, err := testHandler.importWorkspacePackage(ctx, tx, ws, user, p); err != nil {
		t.Fatal(err)
	}
	gotMCP, err := q.GetWorkspaceMcpServer(ctx, db.GetWorkspaceMcpServerParams{ID: mcp.ID, WorkspaceID: ws})
	if err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(json.RawMessage(gotMCP.Config), p.MCPs[0].Config) {
		t.Fatal("MCP update failed")
	}
}

type packageReadRecorder struct {
	db.DBTX
	calls []string
}

func (q *packageReadRecorder) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	q.calls = append(q.calls, sql)
	return q.DBTX.Query(ctx, sql, args...)
}
func (q *packageReadRecorder) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	q.calls = append(q.calls, sql)
	return q.DBTX.QueryRow(ctx, sql, args...)
}

func TestWorkspacePackageBoundedInventoryAndSelectedReads(t *testing.T) {
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := testHandler.Queries.WithTx(tx)
	ws, user := parseUUID(testWorkspaceID), parseUUID(testUserID)
	large := strings.Repeat("excluded-large-content", 50000)
	p := emptyWorkspacePackage()
	var selected string
	for i := 0; i < 105; i++ {
		name, content := fmt.Sprintf("bounded-%03d", i), "small"
		files := []CreateSkillFileRequest{}
		if i == 104 {
			content = large
			files = append(files, CreateSkillFileRequest{Path: "large.txt", Content: large})
		}
		saved, err := q.CreateSkill(ctx, db.CreateSkillParams{WorkspaceID: ws, CreatedBy: user, Name: name, Content: content, Config: []byte(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if _, err := q.UpsertSkillFile(ctx, db.UpsertSkillFileParams{SkillID: saved.ID, Path: file.Path, Content: file.Content}); err != nil {
				t.Fatal(err)
			}
		}
		if i == 0 {
			selected = uuidToString(saved.ID)
		}
		p.Skills = append(p.Skills, PackageSkill{ID: uuidToString(saved.ID), Name: name, Content: content, Files: files})
	}
	recorder := &packageReadRecorder{DBTX: tx}
	h := *testHandler
	h.Queries = db.New(recorder)
	url := "/api/workspaces/" + testWorkspaceID + "/package?category=skills&limit=10000"
	response := testutil.Call(t, h.WorkspacePackageContents, withURLParam(newRequest("GET", url, nil), "id", testWorkspaceID)).Want(200)
	var inventory PackageInventory
	response.JSON(&inventory)
	if len(inventory.Items) != 100 || inventory.Limit != 100 || inventory.Counts.Skills < 105 || response.Body.Len() > 20000 {
		t.Fatal("inventory did not cap summaries")
	}
	next := testutil.Call(t, h.WorkspacePackageContents, withURLParam(newRequest("GET", url+"&offset=100", nil), "id", testWorkspaceID)).Want(200)
	next.JSON(&inventory)
	if len(inventory.Items) == 0 || inventory.Items[0].ID == p.Skills[0].ID {
		t.Fatal("inventory pagination repeated the first page")
	}
	for _, sql := range recorder.calls {
		if strings.Contains(sql, "-- name: ListSkillFiles") || strings.Contains(sql, "-- name: ListIssues") || strings.Contains(sql, "-- name: GetPluginPackageFile") || strings.Contains(sql, "-- name: ListSkillsByWorkspace") {
			t.Fatal("inventory read package content")
		}
	}
	recorder.calls = nil
	exported, err := h.exportWorkspacePackage(ctx, ws, PackageSelection{Skills: []string{selected}})
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.Skills) != 1 || exported.Skills[0].Content != "small" || len(recorder.calls) != 2 || !strings.Contains(recorder.calls[0], "-- name: GetSkillInWorkspace") || !strings.Contains(recorder.calls[1], "-- name: ListSkillFiles") {
		t.Fatal("selected export fetched excluded categories or files")
	}
	// Inspection counts all issues but returns only one capped skill summary page.
	for i := 0; i < 501; i++ {
		p.Issues = append(p.Issues, PackageIssue{ID: fmt.Sprint(i), Title: "Count only", Status: "backlog", Priority: "none"})
	}
	inspected := testutil.Call(t, h.InspectWorkspacePackage, withURLParam(newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/import/inspect?category=skills&limit=10000", ImportWorkspacePackageRequest{Package: p}), "id", testWorkspaceID)).Want(200)
	inspected.JSON(&inventory)
	if len(inventory.Items) != 100 || inventory.Counts.Issues != 501 || inspected.Body.Len() > 20000 || strings.Contains(inspected.Body.String(), "excluded-large-content") {
		t.Fatal("inspection echoed uploaded content")
	}
}
