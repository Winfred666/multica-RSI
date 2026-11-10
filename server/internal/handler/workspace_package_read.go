package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The checklist loads counts and one optional summary page, never package bodies.
func (h *Handler) WorkspacePackageContents(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.packageWorkspaceAccess(w, r)
	if !ok {
		return
	}
	inventory, ok := packageInventoryPage(w, r)
	if !ok {
		return
	}
	counts, err := h.Queries.GetWorkspacePackageCounts(r.Context(), ws)
	if err != nil {
		writeError(w, 500, "failed to read workspace inventory")
		return
	}
	inventory.Counts = counts
	if !h.pluginsV1Enabled(r.Context()) {
		inventory.Counts.Plugins = 0
	}
	if inventory.Category != "" && (inventory.Category != "plugins" || h.pluginsV1Enabled(r.Context())) {
		rows, err := h.Queries.ListWorkspacePackageSummaries(r.Context(), db.ListWorkspacePackageSummariesParams{WorkspaceID: ws, Category: inventory.Category, Limit: int32(inventory.Limit), Offset: int32(inventory.Offset)})
		if err != nil {
			writeError(w, 500, "failed to read resource summaries")
			return
		}
		for _, row := range rows {
			inventory.Items = append(inventory.Items, PackageSummary{ID: uuidToString(row.ID), Name: row.Name, IsSystemSkill: row.IsSystemSkill, Key: row.Key})
		}
	}
	writeJSON(w, 200, inventory)
}
func (h *Handler) ExportWorkspacePackage(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.packageWorkspaceAccess(w, r)
	if !ok {
		return
	}
	var selection PackageSelection
	if !decodePackageRequest(w, r, &selection) {
		return
	}
	if len(selection.Plugins) > 0 && !h.requirePluginsV1(w, r) {
		return
	}
	p, err := h.exportWorkspacePackage(r.Context(), ws, selection)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, errWorkspacePackageInvalid) {
			writeError(w, 400, "selection contains an unknown resource ID")
		} else {
			writeError(w, 500, "failed to export workspace package")
		}
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="workspace.multica.json"`)
	writeJSON(w, 200, p)
}

func (h *Handler) exportWorkspacePackage(ctx context.Context, ws pgtype.UUID, selection PackageSelection) (WorkspacePackage, error) {
	p := emptyWorkspacePackage()
	for _, ids := range []*[]string{&selection.Projects, &selection.Issues, &selection.Agents, &selection.Skills, &selection.MCPs, &selection.Plugins} {
		*ids = slices.Clone(*ids)
		for i, id := range *ids {
			uuid, err := util.ParseUUID(id)
			if err != nil {
				return p, errWorkspacePackageInvalid
			}
			(*ids)[i] = uuidToString(uuid)
		}
	}
	if selection.SystemPrompt {
		workspace, err := h.Queries.GetWorkspace(ctx, ws)
		if err != nil {
			return p, err
		}
		p.SystemPrompt = textToPtr(workspace.Context)
	}
	if len(selection.Projects) > 0 {
		ids := packageUUIDs(selection.Projects)
		projects, err := h.Queries.ListProjects(ctx, db.ListProjectsParams{WorkspaceID: ws, Ids: ids})
		if err != nil {
			return p, err
		}
		resources, err := h.Queries.ListProjectResourcesForProjects(ctx, db.ListProjectResourcesForProjectsParams{WorkspaceID: ws, ProjectIds: ids})
		if err != nil {
			return p, err
		}
		byProject := map[pgtype.UUID][]PackageProjectResource{}
		for _, resource := range resources {
			byProject[resource.ProjectID] = append(byProject[resource.ProjectID], PackageProjectResource{ResourceType: resource.ResourceType, ResourceRef: resource.ResourceRef, Label: textToPtr(resource.Label), Position: resource.Position})
		}
		for _, project := range projects {
			entry := PackageProject{ID: uuidToString(project.ID), Title: project.Title, Description: textToPtr(project.Description), Icon: textToPtr(project.Icon), Status: project.Status, Priority: project.Priority, StartDate: project.StartDate, DueDate: project.DueDate, Resources: append([]PackageProjectResource{}, byProject[project.ID]...)}
			p.Projects = append(p.Projects, entry)
		}
	}
	if len(selection.Issues) > 0 {
		ids := packageUUIDs(selection.Issues)
		// Use the existing paginated query without truncating large workspaces.
		for offset := int32(0); ; offset += 500 {
			issues, err := h.Queries.ListIssues(ctx, db.ListIssuesParams{WorkspaceID: ws, Ids: ids, Limit: 500, Offset: offset})
			if err != nil {
				return p, err
			}
			for _, issue := range issues {
				entry := PackageIssue{ID: uuidToString(issue.ID), Title: issue.Title, Description: textToPtr(issue.Description), Status: issue.Status, Priority: issue.Priority, Position: issue.Position, StartDate: issue.StartDate, DueDate: issue.DueDate, Properties: issue.Properties, Metadata: issue.Metadata}
				if issue.ProjectID.Valid {
					entry.ProjectID = uuidToString(issue.ProjectID)
				}
				if issue.ParentIssueID.Valid {
					entry.ParentIssueID = uuidToString(issue.ParentIssueID)
				}
				if issue.AssigneeType.String == "agent" && issue.AssigneeID.Valid {
					entry.AgentID = uuidToString(issue.AssigneeID)
				}
				if issue.Stage.Valid {
					stage := issue.Stage.Int32
					entry.Stage = &stage
				}
				p.Issues = append(p.Issues, entry)
			}
			if len(issues) < 500 {
				break
			}
		}
		statuses, err := h.Queries.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{WorkspaceID: ws, IncludeArchived: true})
		if err != nil {
			return p, err
		}
		for _, status := range statuses {
			p.IssueStatuses = append(p.IssueStatuses, PackageIssueStatus{Key: status.Key, Name: status.Name, Description: status.Description, Category: status.Category, Color: status.Color, Icon: status.Icon})
		}
	}
	for _, id := range selection.Skills {
		s, err := h.Queries.GetSkillInWorkspace(ctx, db.GetSkillInWorkspaceParams{ID: parseUUID(id), WorkspaceID: ws})
		if err != nil {
			return p, err
		}
		entry := PackageSkill{ID: uuidToString(s.ID), Name: s.Name, Description: s.Description, Content: s.Content, Config: decodeSkillConfig(s.Config), IsSystemSkill: s.IsSystemSkill, Files: []CreateSkillFileRequest{}}
		files, err := h.Queries.ListSkillFiles(ctx, s.ID)
		if err != nil {
			return p, err
		}
		for _, file := range files {
			entry.Files = append(entry.Files, CreateSkillFileRequest{Path: file.Path, Content: file.Content})
		}
		p.Skills = append(p.Skills, entry)
	}
	for _, id := range selection.Agents {
		a, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: parseUUID(id), WorkspaceID: ws})
		if err != nil {
			return p, err
		}
		if a.ArchivedAt.Valid {
			return p, pgx.ErrNoRows
		}
		entry := PackageAgent{ID: uuidToString(a.ID), Name: a.Name, Description: a.Description, Instructions: a.Instructions, RuntimeMode: a.RuntimeMode, Model: textToPtr(a.Model), ThinkingLevel: textToPtr(a.ThinkingLevel), ServiceTier: textToPtr(a.ServiceTier), MaxConcurrentTasks: a.MaxConcurrentTasks, CustomArgs: a.CustomArgs, MCPConfig: a.McpConfig, ConversationStarters: a.ConversationStarters, Skills: []PackageBinding{}, MCPs: []PackageBinding{}}
		skills, err := h.Queries.ListAgentSkillSummaries(ctx, a.ID)
		if err != nil {
			return p, err
		}
		for _, s := range skills {
			entry.Skills = append(entry.Skills, PackageBinding{ID: uuidToString(s.ID), Enabled: s.Enabled})
		}
		mcps, err := h.Queries.ListAgentMcpBindings(ctx, a.ID)
		if err != nil {
			return p, err
		}
		for _, m := range mcps {
			entry.MCPs = append(entry.MCPs, PackageBinding{ID: uuidToString(m.ServerID), Enabled: m.Enabled})
		}
		p.Agents = append(p.Agents, entry)
	}
	for _, id := range selection.MCPs {
		m, err := h.Queries.GetWorkspaceMcpServer(ctx, db.GetWorkspaceMcpServerParams{ID: parseUUID(id), WorkspaceID: ws})
		if err != nil {
			return p, err
		}
		p.MCPs = append(p.MCPs, PackageMCP{ID: uuidToString(m.ID), Name: m.Name, Config: m.Config})
	}
	for _, id := range selection.Plugins {
		plugin, err := h.Queries.GetWorkspacePluginInstallation(ctx, db.GetWorkspacePluginInstallationParams{ID: parseUUID(id), WorkspaceID: ws})
		if err != nil {
			return p, err
		}
		files := map[string][]byte{"multica.plugin.json": plugin.Manifest}
		paths, err := h.Queries.ListPluginPackageFilePaths(ctx, plugin.PackageVersionID)
		if err != nil {
			return p, err
		}
		for _, file := range paths {
			row, err := h.Queries.GetPluginPackageFile(ctx, db.GetPluginPackageFileParams{VersionID: plugin.PackageVersionID, Path: file.Path})
			if err != nil {
				return p, err
			}
			files[file.Path] = row.Content
		}
		bundle, err := packageZip(files)
		if err != nil {
			return p, err
		}
		var metadata struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(plugin.Manifest, &metadata); err != nil {
			return p, err
		}
		config := map[string]any{}
		if err := json.Unmarshal(plugin.Config, &config); err != nil {
			return p, err
		}
		p.Plugins = append(p.Plugins, PackagePlugin{ID: uuidToString(plugin.ID), Key: plugin.PluginKey, Name: metadata.Name, Bundle: bundle, Config: config, Enabled: plugin.Enabled})
	}
	selected, err := selectedPackage(p, &selection)
	if err != nil {
		return p, errWorkspacePackageInvalid
	}
	return selected, nil
}

// Export IDs have already been validated and normalized above.
func packageUUIDs(ids []string) []pgtype.UUID {
	result := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		result[i] = parseUUID(id)
	}
	return result
}

// The same count/summary response describes stored inventory and an uploaded package.
type PackageSummary struct {
	Key           string `json:"key,omitempty"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	IsSystemSkill bool   `json:"is_system_skill,omitempty"`
}
type PackageInventory struct {
	Counts   db.GetWorkspacePackageCountsRow `json:"counts"`
	Category string                          `json:"category,omitempty"`
	Items    []PackageSummary                `json:"items,omitempty"`
	Limit    int                             `json:"limit,omitempty"`
	Offset   int                             `json:"offset,omitempty"`
}

func packageInventoryPage(w http.ResponseWriter, r *http.Request) (PackageInventory, bool) {
	page := PackageInventory{Category: r.URL.Query().Get("category")}
	if page.Category == "" {
		return page, true
	}
	switch page.Category {
	case "projects", "issues", "agents", "skills", "mcps", "plugins":
	default:
		writeError(w, 400, "category must be projects, issues, agents, skills, mcps or plugins")
		return page, false
	}
	// Match existing list endpoints' limit/offset convention and cap.
	page.Limit = 50
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		page.Limit = min(n, 100)
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n >= 0 {
		page.Offset = min(n, 2147483647)
	}
	return page, true
}
func packageCounts(p WorkspacePackage) db.GetWorkspacePackageCountsRow {
	return db.GetWorkspacePackageCountsRow{Projects: int64(len(p.Projects)), Issues: int64(len(p.Issues)), Agents: int64(len(p.Agents)), Skills: int64(len(p.Skills)), Mcps: int64(len(p.MCPs)), Plugins: int64(len(p.Plugins)), SystemPrompt: p.SystemPrompt != nil}
}
func packageInspection(w http.ResponseWriter, r *http.Request, p WorkspacePackage) (PackageInventory, bool) {
	page, ok := packageInventoryPage(w, r)
	if !ok {
		return page, false
	}
	page.Counts = packageCounts(p)
	index := 0
	add := func(id, name string, system bool) {
		if index >= page.Offset && len(page.Items) < page.Limit {
			page.Items = append(page.Items, PackageSummary{ID: id, Name: name, IsSystemSkill: system})
		}
		index++
	}
	switch page.Category {
	case "projects":
		for _, v := range p.Projects {
			add(v.ID, v.Title, false)
		}
	case "issues":
		for _, v := range p.Issues {
			add(v.ID, v.Title, false)
		}
	case "agents":
		for _, v := range p.Agents {
			add(v.ID, v.Name, false)
		}
	case "skills":
		for _, v := range p.Skills {
			add(v.ID, v.Name, v.IsSystemSkill)
		}
	case "mcps":
		for _, v := range p.MCPs {
			add(v.ID, v.Name, false)
		}
	case "plugins":
		for _, v := range p.Plugins {
			add(v.ID, v.Name, false)
		}
	}
	return page, true
}
