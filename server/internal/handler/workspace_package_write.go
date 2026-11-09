package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/agentconfig"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

func packageJSON(raw json.RawMessage, fallback string) []byte {
	if len(raw) == 0 || string(raw) == "null" {
		return []byte(fallback)
	}
	return raw
}
func (h *Handler) importWorkspacePackage(ctx context.Context, tx pgx.Tx, ws, user pgtype.UUID, p WorkspacePackage) (PackageImportResult, error) {
	result := PackageImportResult{Projects: map[string]string{}, Issues: map[string]string{}, Agents: map[string]string{}, Skills: map[string]string{}, MCPs: map[string]string{}, Plugins: map[string]string{}}
	if err := validateWorkspacePackage(p); err != nil {
		return result, fmt.Errorf("%w: %v", errWorkspacePackageInvalid, err)
	}
	q := h.Queries.WithTx(tx)
	var issuePolicy service.IssueCountPolicy
	if len(p.Issues) > 0 {
		issuePolicy = service.ResolveIssueCountPolicy(ctx, h.Entitlements, ws)
	}
	if _, err := q.LockWorkspaceForChatSessionCreate(ctx, ws); err != nil {
		return result, err
	}
	// Existing writers use nested pgx transactions (savepoints) in this import.
	writer := *h
	writer.Queries = q
	writer.TxStarter = tx
	for _, s := range p.Skills {
		previous, err := q.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{WorkspaceID: ws, Name: s.Name})
		var saved SkillWithFilesResponse
		if errors.Is(err, pgx.ErrNoRows) {
			saved, err = createSkillWithFilesInTx(ctx, q, skillCreateInput{WorkspaceID: ws, CreatorID: user, Name: s.Name, Description: s.Description, Content: s.Content, Config: s.Config, Files: s.Files, IsSystemSkill: s.IsSystemSkill})
		} else if err == nil {
			if previous.IsSystemSkill != s.IsSystemSkill {
				return result, fmt.Errorf("%w: skill %s cannot change system-skill classification on import", errWorkspacePackageConflict, s.Name)
			}
			saved, err = writer.overwriteSkillWithFiles(ctx, skillOverwriteInput{WorkspaceID: ws, TargetSkillID: previous.ID, UserID: uuidToString(user), ExpectedName: s.Name, AllowOverwrite: func(_ string, skill db.Skill) bool { return skill.IsSystemSkill == s.IsSystemSkill }, Description: s.Description, Content: s.Content, Config: s.Config, Files: s.Files})
		}

		if err != nil {
			return result, fmt.Errorf("skill %s: %w", s.Name, err)
		}
		result.Skills[s.ID] = saved.ID
	}
	if len(p.MCPs) > 0 {
		existingMCPs, err := q.ListWorkspaceMcpServers(ctx, ws)
		if err != nil {
			return result, err
		}
		for _, m := range p.MCPs {
			var previous pgtype.UUID
			for _, row := range existingMCPs {
				if row.Name == m.Name {
					previous = row.ID
					break
				}
			}
			var saved db.WorkspaceMcpServer
			if previous.Valid {
				saved, err = q.UpdateWorkspaceMcpServer(ctx, db.UpdateWorkspaceMcpServerParams{ID: previous, WorkspaceID: ws, Config: m.Config})
			} else {
				saved, err = q.CreateWorkspaceMcpServer(ctx, db.CreateWorkspaceMcpServerParams{WorkspaceID: ws, Name: m.Name, Config: m.Config, CreatedBy: user})
			}
			if err != nil {
				return result, err
			}
			result.MCPs[m.ID] = uuidToString(saved.ID)
		}
	}
	for _, a := range p.Agents {
		limit := a.MaxConcurrentTasks
		if limit == 0 {
			limit = agentconfig.DefaultMaxConcurrentTasks
		}
		saved, err := q.CreateAgent(ctx, db.CreateAgentParams{WorkspaceID: ws, OwnerID: user, Name: a.Name, Description: a.Description, Instructions: a.Instructions, RuntimeMode: a.RuntimeMode, RuntimeConfig: []byte("{}"), Visibility: "private", PermissionMode: "private", MaxConcurrentTasks: limit, Model: ptrToText(a.Model), ThinkingLevel: ptrToText(a.ThinkingLevel), ServiceTier: ptrToText(a.ServiceTier), CustomArgs: packageJSON(a.CustomArgs, "[]"), CustomEnv: []byte("{}"), McpConfig: a.MCPConfig, ConversationStarters: packageJSON(a.ConversationStarters, "[]")})
		if err != nil {
			return result, fmt.Errorf("agent %s: %w", a.Name, err)
		}
		result.Agents[a.ID] = uuidToString(saved.ID)
		if err := q.AddSystemSkillsToAgent(ctx, saved.ID); err != nil {
			return result, err
		}
		for _, binding := range a.Skills {
			if id := result.Skills[binding.ID]; id != "" {
				if err := q.AddAgentSkill(ctx, db.AddAgentSkillParams{AgentID: saved.ID, SkillID: parseUUID(id)}); err != nil {
					return result, err
				}
				if _, err := q.SetAgentSkillEnabled(ctx, db.SetAgentSkillEnabledParams{AgentID: saved.ID, SkillID: parseUUID(id), Enabled: binding.Enabled}); err != nil {
					return result, err
				}
			}
		}
		for _, binding := range a.MCPs {
			if id := result.MCPs[binding.ID]; id != "" {
				if err := q.AddAgentMcpServer(ctx, db.AddAgentMcpServerParams{AgentID: saved.ID, ServerID: parseUUID(id)}); err != nil {
					return result, err
				}
				if _, err := q.SetAgentMcpServerEnabled(ctx, db.SetAgentMcpServerEnabledParams{AgentID: saved.ID, ServerID: parseUUID(id), Enabled: binding.Enabled}); err != nil {
					return result, err
				}
			}
		}
	}
	if len(p.Projects) > 0 || len(p.Issues) > 0 {
		if err := issuestatus.Ensure(ctx, q, ws); err != nil {
			return result, err
		}
		for _, s := range p.IssueStatuses {
			previous, err := q.GetIssueStatusEntryByKey(ctx, db.GetIssueStatusEntryByKeyParams{WorkspaceID: ws, Key: s.Key})
			if errors.Is(err, pgx.ErrNoRows) {
				var badRequest string
				_, badRequest, err = writer.createIssueStatusEntry(ctx, ws, s.Category, db.CreateIssueStatusEntryParams{WorkspaceID: ws, Key: s.Key, Name: s.Name, Description: s.Description, Category: s.Category, Color: s.Color, Icon: s.Icon})
				if badRequest != "" {
					return result, fmt.Errorf("%w: %s", errWorkspacePackageInvalid, badRequest)
				}
			} else if err == nil && previous.Category != s.Category {
				return result, fmt.Errorf("%w: issue status %s has a different category", errWorkspacePackageConflict, s.Key)
			}
			if err != nil {
				return result, err
			}
		}
	}
	for _, project := range p.Projects {
		saved, err := q.CreateProject(ctx, db.CreateProjectParams{WorkspaceID: ws, Title: project.Title, Description: ptrToText(project.Description), Icon: ptrToText(project.Icon), Status: project.Status, Priority: project.Priority, StartDate: project.StartDate, DueDate: project.DueDate, CreatorType: pgtype.Text{String: "member", Valid: true}, CreatorID: user})
		if err != nil {
			return result, err
		}
		result.Projects[project.ID] = uuidToString(saved.ID)
		for _, resource := range project.Resources {
			normalized, err := normalizePackageResource(resource)
			if err != nil {
				return result, err
			}
			if _, err := q.CreateProjectResource(ctx, db.CreateProjectResourceParams{ProjectID: saved.ID, WorkspaceID: ws, ResourceType: resource.ResourceType, ResourceRef: normalized, Label: ptrToText(resource.Label), Position: resource.Position, CreatedBy: user}); err != nil {
				return result, err
			}
		}
	}
	ref := func(ids map[string]string, id string) pgtype.UUID {
		if value := ids[id]; value != "" {
			return parseUUID(value)
		}
		return pgtype.UUID{}
	}
	for _, issue := range p.Issues {
		status, err := issuestatus.Resolve(ctx, q, ws, issue.Status)
		if err != nil {
			return result, err
		}
		number, err := service.AllocateIssueNumber(ctx, q, ws, issuePolicy)
		if err != nil {
			return result, err
		}
		var assigneeType pgtype.Text
		assignee := ref(result.Agents, issue.AgentID)
		if assignee.Valid {
			assigneeType = pgtype.Text{String: "agent", Valid: true}
		}
		saved, err := q.CreateIssue(ctx, db.CreateIssueParams{ID: dbid.NewV7(), WorkspaceID: ws, Title: issue.Title, Description: ptrToText(issue.Description), Status: status.Key, Priority: issue.Priority, CreatorType: "member", CreatorID: user, ProjectID: ref(result.Projects, issue.ProjectID), AssigneeType: assigneeType, AssigneeID: assignee, Position: issue.Position, StartDate: issue.StartDate, DueDate: issue.DueDate, Stage: ptrToInt4(issue.Stage), Properties: []byte("{}"), Number: number})
		if err != nil {
			return result, err
		}
		result.Issues[issue.ID] = uuidToString(saved.ID)
		var metadata map[string]json.RawMessage
		if err := json.Unmarshal(packageJSON(issue.Metadata, "{}"), &metadata); err != nil {
			return result, err
		}
		for key, value := range metadata {
			if _, err := q.SetIssueMetadataKey(ctx, db.SetIssueMetadataKeyParams{ID: saved.ID, WorkspaceID: ws, Key: key, Value: value}); err != nil {
				return result, err
			}
		}
	}
	// Link children after every parent exists, regardless of package order.
	for _, issue := range p.Issues {
		if issue.ParentIssueID != "" {
			assignee := ref(result.Agents, issue.AgentID)
			var assigneeType pgtype.Text
			if assignee.Valid {
				assigneeType = pgtype.Text{String: "agent", Valid: true}
			}
			if _, err := q.UpdateIssue(ctx, db.UpdateIssueParams{ID: ref(result.Issues, issue.ID), ParentIssueID: ref(result.Issues, issue.ParentIssueID), ProjectID: ref(result.Projects, issue.ProjectID), AssigneeID: assignee, AssigneeType: assigneeType, StartDate: issue.StartDate, DueDate: issue.DueDate, Stage: ptrToInt4(issue.Stage)}); err != nil {
				return result, err
			}
		}
	}
	if len(p.Plugins) > 0 {
		plugins := *h.PluginService
		plugins.Queries = q
		plugins.TxStarter = tx
		packages, err := plugins.ListPackages(ctx, ws)
		if err != nil {
			return result, err
		}
		versions := map[string]service.PluginPackageVersionSummary{}
		for _, pkg := range packages {
			for _, version := range pkg.Versions {
				versions[pkg.PluginKey+"/"+version.Version] = version
			}
		}
		for _, plugin := range p.Plugins {
			bundle, err := plugincontract.ParseBundle(plugin.Bundle)
			if err != nil {
				return result, err
			}
			key := bundle.Manifest.Key + "/" + bundle.Manifest.Version
			version := versions[key]
			if version.ID != "" && version.Digest != service.BundleDigest(bundle) {
				stored, err := plugins.VersionForWorkspace(ctx, ws, version.ID)
				if err != nil {
					return result, err
				}
				matches, err := packagePluginVersionMatches(ctx, q, stored, bundle)
				if err != nil {
					return result, err
				}
				if !matches {
					return result, &service.PluginError{Kind: service.PluginErrorConflict, Message: fmt.Sprintf("plugin %s version already exists with different contents", bundle.Manifest.Key)}
				}
			}
			if version.ID == "" {
				published, err := plugins.PublishBundle(ctx, ws, user, plugin.Bundle)
				if err != nil {
					return result, err
				}
				for _, v := range published.Versions {
					versions[published.PluginKey+"/"+v.Version] = v
				}
				version = versions[key]
			}
			versionID := version.ID

			saved, err := plugins.InstallPlugin(ctx, ws, user, versionID, bundle.Manifest.Scopes)
			if err != nil {
				return result, err
			}
			saved, err = plugins.SetConfig(ctx, saved, plugin.Config)
			if err != nil {
				return result, err
			}
			saved, err = plugins.SetEnabled(ctx, saved, plugin.Enabled)
			if err != nil {
				return result, err
			}
			result.Plugins[plugin.ID] = uuidToString(saved.ID)
		}
	}
	if p.SystemPrompt != nil {
		if _, err := q.UpdateWorkspace(ctx, db.UpdateWorkspaceParams{ID: ws, Context: ptrToText(p.SystemPrompt)}); err != nil {
			return result, err
		}
	}
	return result, nil
}

// JSONB changes manifest key order; compare only this selected published version.
func packagePluginVersionMatches(ctx context.Context, q *db.Queries, stored db.PluginPackageVersion, bundle plugincontract.Bundle) (bool, error) {
	var manifests [2]any
	for i, raw := range [][]byte{stored.Manifest, bundle.Canonical} {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&manifests[i]); err != nil {
			return false, err
		}
	}
	if !jsonEqual(manifests[0], manifests[1]) {
		return false, nil
	}
	files, err := q.ListPluginPackageFilePaths(ctx, stored.ID)
	if err != nil {
		return false, err
	}
	if len(files) != len(bundle.Files) {
		return false, nil
	}
	digests := make(map[string]string, len(files))
	for _, file := range files {
		digests[file.Path] = file.Sha256
	}
	for _, file := range bundle.Files {
		// Reuse published file hashes instead of rereading stored file bodies.
		sum := sha256.Sum256(file.Content)
		if digests[file.Path] != hex.EncodeToString(sum[:]) {
			return false, nil
		}
	}
	return true, nil
}
