package handler

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/harness"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	skillpkg "github.com/multica-ai/multica/server/internal/skill"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

var errWorkspacePackageInvalid = errors.New("invalid workspace package")

var errWorkspacePackageConflict = errors.New("workspace package conflicts with the destination")

// IDs are package-local references. New resources receive destination IDs.
type WorkspacePackage struct {
	SchemaVersion int                  `json:"schema_version"`
	Projects      []PackageProject     `json:"projects"`
	Issues        []PackageIssue       `json:"issues"`
	IssueStatuses []PackageIssueStatus `json:"issue_statuses,omitempty"`
	Agents        []PackageAgent       `json:"agents"`
	Skills        []PackageSkill       `json:"skills"`
	MCPs          []PackageMCP         `json:"mcps"`
	Plugins       []PackagePlugin      `json:"plugins"`
	SystemPrompt  *string              `json:"system_prompt"`
}

// Only catalog fields travel; workspace identity and audit timestamps do not.
type PackageIssueStatus struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Color       string `json:"color"`
	Icon        string `json:"icon"`
}
type PackageProject struct {
	ID          string                   `json:"id"`
	Title       string                   `json:"title"`
	Description *string                  `json:"description"`
	Icon        *string                  `json:"icon"`
	Status      string                   `json:"status"`
	Priority    string                   `json:"priority"`
	StartDate   pgtype.Date              `json:"start_date"`
	DueDate     pgtype.Date              `json:"due_date"`
	Resources   []PackageProjectResource `json:"resources"`
}
type PackageProjectResource struct {
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
	Label        *string         `json:"label"`
	Position     int32           `json:"position"`
}
type PackageIssue struct {
	ID            string          `json:"id"`
	Title         string          `json:"title"`
	Description   *string         `json:"description"`
	Status        string          `json:"status"`
	Priority      string          `json:"priority"`
	ParentIssueID string          `json:"parent_issue_id,omitempty"`
	ProjectID     string          `json:"project_id,omitempty"`
	AgentID       string          `json:"agent_id,omitempty"`
	Position      float64         `json:"position"`
	StartDate     pgtype.Date     `json:"start_date"`
	DueDate       pgtype.Date     `json:"due_date"`
	Stage         *int32          `json:"stage"`
	Properties    json.RawMessage `json:"properties"`
	Metadata      json.RawMessage `json:"metadata"`
}
type PackageBinding struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}
type PackageAgent struct {
	ID                   string           `json:"id"`
	Name                 string           `json:"name"`
	Description          string           `json:"description"`
	Instructions         string           `json:"instructions"`
	RuntimeMode          string           `json:"runtime_mode"`
	Model                *string          `json:"model"`
	ThinkingLevel        *string          `json:"thinking_level"`
	ServiceTier          *string          `json:"service_tier"`
	MaxConcurrentTasks   int32            `json:"max_concurrent_tasks"`
	CustomArgs           json.RawMessage  `json:"custom_args"`
	MCPConfig            json.RawMessage  `json:"mcp_config"`
	ConversationStarters json.RawMessage  `json:"conversation_starters"`
	Skills               []PackageBinding `json:"skills"`
	MCPs                 []PackageBinding `json:"mcps"`
}
type PackageSkill struct {
	ID            string                   `json:"id"`
	Name          string                   `json:"name"`
	Description   string                   `json:"description"`
	Content       string                   `json:"content"`
	Config        any                      `json:"config"`
	IsSystemSkill bool                     `json:"is_system_skill"`
	Files         []CreateSkillFileRequest `json:"files"`
}
type PackageMCP struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}
type PackagePlugin struct {
	ID      string         `json:"id"`
	Key     string         `json:"key"`
	Name    string         `json:"name"`
	Bundle  []byte         `json:"bundle"` // JSON base64 preserves binary package assets.
	Config  map[string]any `json:"config"`
	Enabled bool           `json:"enabled"`
}
type PackageSelection struct {
	ProjectsIssues bool     `json:"projects_issues"`
	Agents         []string `json:"agents"`
	Skills         []string `json:"skills"`
	MCPs           []string `json:"mcps"`
	Plugins        []string `json:"plugins"`
	SystemPrompt   bool     `json:"system_prompt"`
}
type ImportWorkspacePackageRequest struct {
	Package              WorkspacePackage  `json:"package"`
	Selection            *PackageSelection `json:"selection,omitempty"`
	PluginExecutionToken string            `json:"plugin_execution_token,omitempty"`
}
type PackageImportResult struct {
	Projects map[string]string `json:"projects"`
	Issues   map[string]string `json:"issues"`
	Agents   map[string]string `json:"agents"`
	Skills   map[string]string `json:"skills"`
	MCPs     map[string]string `json:"mcps"`
	Plugins  map[string]string `json:"plugins"`
}

func emptyWorkspacePackage() WorkspacePackage {
	return WorkspacePackage{SchemaVersion: 1, Projects: []PackageProject{}, Issues: []PackageIssue{}, Agents: []PackageAgent{}, Skills: []PackageSkill{}, MCPs: []PackageMCP{}, Plugins: []PackagePlugin{}}
}
func selectedPackage(p WorkspacePackage, selection *PackageSelection) (WorkspacePackage, error) {
	if p.SchemaVersion != 1 {
		return p, fmt.Errorf("unsupported workspace package schema_version")
	}
	if selection == nil {
		return p, nil
	}
	result := emptyWorkspacePackage()
	if selection.ProjectsIssues {
		result.Projects = p.Projects
		result.Issues = p.Issues
		result.IssueStatuses = p.IssueStatuses
	}
	if selection.SystemPrompt {
		result.SystemPrompt = p.SystemPrompt
	}
	pick := func(ids []string, visit func(func(string) bool)) error {
		wanted := map[string]bool{}
		for _, id := range ids {
			wanted[id] = true
		}
		visit(func(id string) bool {
			if wanted[id] {
				delete(wanted, id)
				return true
			}
			return false
		})
		if len(wanted) > 0 {
			return fmt.Errorf("selection contains an unknown resource ID")
		}
		return nil
	}
	if err := pick(selection.Agents, func(has func(string) bool) {
		for _, v := range p.Agents {
			if has(v.ID) {
				result.Agents = append(result.Agents, v)
			}
		}
	}); err != nil {
		return result, err
	}
	if err := pick(selection.Skills, func(has func(string) bool) {
		for _, v := range p.Skills {
			if has(v.ID) {
				result.Skills = append(result.Skills, v)
			}
		}
	}); err != nil {
		return result, err
	}
	if err := pick(selection.MCPs, func(has func(string) bool) {
		for _, v := range p.MCPs {
			if has(v.ID) {
				result.MCPs = append(result.MCPs, v)
			}
		}
	}); err != nil {
		return result, err
	}
	if err := pick(selection.Plugins, func(has func(string) bool) {
		for _, v := range p.Plugins {
			if has(v.ID) {
				result.Plugins = append(result.Plugins, v)
			}
		}
	}); err != nil {
		return result, err
	}
	return result, nil
}
func validateWorkspacePackage(p WorkspacePackage) error {
	if p.SchemaVersion != 1 {
		return fmt.Errorf("unsupported workspace package schema_version")
	}
	checkID := func(seen map[string]bool, id string) error {
		if id == "" || seen[id] {
			return fmt.Errorf("resource IDs must be nonempty and unique within each category")
		}
		seen[id] = true
		return nil
	}
	seen := map[string]bool{}
	for _, s := range p.Skills {
		if err := checkID(seen, s.ID); err != nil {
			return err
		}
		if strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.Content) == "" {
			return fmt.Errorf("skill name and content are required")
		}
		for _, f := range s.Files {
			if !validateFilePath(f.Path) {
				return fmt.Errorf("invalid skill file path: %s", f.Path)
			}
		}
	}
	seen = map[string]bool{}
	for _, a := range p.Agents {
		if err := checkID(seen, a.ID); err != nil {
			return err
		}
		if a.RuntimeMode != "local" && a.RuntimeMode != "cloud" {
			return fmt.Errorf("agent runtime_mode must be local or cloud")
		}
		if strings.TrimSpace(a.Name) == "" {
			return fmt.Errorf("agent name is required")
		}
		if a.MaxConcurrentTasks != 0 {
			if err := validateAgentMaxConcurrentTasks(a.MaxConcurrentTasks); err != nil {
				return err
			}
		}
	}
	seen = map[string]bool{}
	for _, m := range p.MCPs {
		if err := checkID(seen, m.ID); err != nil {
			return err
		}
		if err := validateWorkspaceMcpServerName(m.Name); err != nil {
			return err
		}
		if err := validateWorkspaceMcpServerEntry(m.Config); err != nil {
			return err
		}
	}
	seen = map[string]bool{}
	for _, v := range p.Projects {
		if err := checkID(seen, v.ID); err != nil {
			return err
		}
		if strings.TrimSpace(v.Title) == "" {
			return fmt.Errorf("project title is required")
		}
		if !slices.Contains(validProjectStatuses, v.Status) || !slices.Contains(validProjectPriorities, v.Priority) {
			return fmt.Errorf("invalid project status or priority")
		}
		for _, r := range v.Resources {
			if _, err := normalizePackageResource(r); err != nil {
				return err
			}
		}
	}
	projects := seen
	seen = map[string]bool{}
	parents := map[string]string{}
	for _, v := range p.Issues {
		if err := checkID(seen, v.ID); err != nil {
			return err
		}
		if strings.TrimSpace(v.Title) == "" {
			return fmt.Errorf("issue title is required")
		}
		if !slices.Contains(validIssuePriorities, v.Priority) {
			return fmt.Errorf("invalid issue priority")
		}
		var properties map[string]json.RawMessage
		if err := json.Unmarshal(packageJSON(v.Properties, "{}"), &properties); err != nil || len(properties) != 0 {
			return fmt.Errorf("issue properties must be empty; workspace property definitions are not portable")
		}
		if v.Stage != nil && *v.Stage < 1 {
			return fmt.Errorf("issue stage must be >= 1")
		}
		var metadata map[string]json.RawMessage
		if err := json.Unmarshal(packageJSON(v.Metadata, "{}"), &metadata); err != nil {
			return fmt.Errorf("invalid issue metadata")
		}
		for key, value := range metadata {
			if err := validateIssueMetadataKey(key); err != nil {
				return err
			}
			if err := validateIssueMetadataValue(value); err != nil {
				return err
			}
		}
		if v.ProjectID != "" && !projects[v.ProjectID] {
			return fmt.Errorf("issue references an unknown project")
		}
		parents[v.ID] = v.ParentIssueID
	}
	// Each issue moves from unseen to visiting to complete once.
	state := map[string]uint8{}
	for id := range parents {
		path := []string{}
		for id != "" && state[id] != 2 {
			if !seen[id] {
				return fmt.Errorf("issue references an unknown parent")
			}
			if state[id] == 1 {
				return fmt.Errorf("issue parent cycle")
			}
			state[id] = 1
			path = append(path, id)
			id = parents[id]
		}
		for _, id := range path {
			state[id] = 2
		}
	}
	for i := range p.IssueStatuses {
		s := &p.IssueStatuses[i]
		if !issuestatus.IsBuiltIn(s.Key) {
			if _, err := issuestatus.ValidateKey(s.Key); err != nil {
				return err
			}
		}
		category, ok := issuestatus.ParseCategory(s.Category)
		if !ok {
			return fmt.Errorf("invalid issue status category")
		}
		s.Category = category
		if strings.TrimSpace(s.Name) == "" || len([]rune(s.Name)) > 64 || len([]rune(s.Description)) > 256 {
			return fmt.Errorf("invalid issue status name or description")
		}
		color, err := normalizeColor(s.Color)
		if err != nil {
			return err
		}
		s.Color = color
		if !validIssueStatusIcon(s.Icon) {
			return fmt.Errorf("invalid issue status icon")
		}
	}
	seen = map[string]bool{}
	for _, v := range p.Plugins {
		if err := checkID(seen, v.ID); err != nil {
			return err
		}
		bundle, err := plugincontract.ParseBundle(v.Bundle)
		if err != nil {
			return err
		}
		if v.Key != bundle.Manifest.Key || v.Name != bundle.Manifest.Name {
			return fmt.Errorf("plugin name/key does not match its bundle")
		}
	}
	if p.SystemPrompt != nil {
		if err := execenv.ValidateStartupTemplate(*p.SystemPrompt); err != nil {
			return err
		}
	}
	return nil
}
func normalizePackageResource(resource PackageProjectResource) (json.RawMessage, error) {
	normalized, err := validateAndNormalizeResourceRef(resource.ResourceType, resource.ResourceRef)
	if err != nil {
		return nil, err
	}
	if resource.ResourceType == "local_directory" {
		return nil, fmt.Errorf("local_directory resources bind a specific machine; remove them from the package and configure them in the destination")
	}
	return normalized, nil
}
func (h *Handler) packageWorkspaceAccess(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	id, ok := parseUUIDOrBadRequest(w, workspaceIDFromURL(r, "id"), "workspace id")
	if !ok {
		return id, pgtype.UUID{}, false
	}
	member, ok := h.workspaceMember(w, r, uuidToString(id))
	if !ok {
		return id, pgtype.UUID{}, false
	}
	if !roleAllowed(member.Role, "owner", "admin") || isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "workspace packages require a workspace owner or admin member")
		return id, pgtype.UUID{}, false
	}
	if actorType, _ := h.resolveActor(r, requestUserID(r), uuidToString(id)); actorType == "agent" {
		writeError(w, http.StatusForbidden, "workspace packages require a member actor")
		return id, pgtype.UUID{}, false
	}
	return id, member.UserID, true
}
func decodePackageRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace package request")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "expected one JSON request")
		return false
	}
	return true
}
func (h *Handler) InspectWorkspacePackage(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.packageWorkspaceAccess(w, r); !ok {
		return
	}
	var req ImportWorkspacePackageRequest
	if !decodePackageRequest(w, r, &req) {
		return
	}
	p, err := selectedPackage(req.Package, req.Selection)
	if err == nil {
		err = validateWorkspacePackage(p)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	inventory, ok := packageInspection(w, r, p)
	if ok {
		writeJSON(w, http.StatusOK, inventory)
	}
}
func (h *Handler) DefaultWorkspacePackage(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.packageWorkspaceAccess(w, r); !ok {
		return
	}
	p, err := defaultWorkspacePackage()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load startup kit")
		return
	}
	if !h.pluginsV1Enabled(r.Context()) {
		p.Plugins = []PackagePlugin{}
	}
	writeJSON(w, http.StatusOK, p)
}
func (h *Handler) ImportWorkspacePackage(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.packageWorkspaceAccess(w, r)
	if !ok {
		return
	}
	var req ImportWorkspacePackageRequest
	if !decodePackageRequest(w, r, &req) {
		return
	}
	p, err := selectedPackage(req.Package, req.Selection)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if (len(p.Plugins) > 0 || req.PluginExecutionToken != "") && !h.requirePluginsV1(w, r) {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to begin workspace import")
		return
	}
	defer tx.Rollback(r.Context())
	_, err = h.importWorkspacePackage(r.Context(), tx, ws, user, p)
	if err == nil && req.PluginExecutionToken != "" {
		plugins := *h.PluginService
		plugins.Queries = h.Queries.WithTx(tx)
		plugins.TxStarter = tx
		_, err = plugins.SetPluginExecution(r.Context(), ws, req.PluginExecutionToken)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		if writeIssueLimitReached(w, err) {
			return
		}
		if errors.Is(err, errWorkspacePackageInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else if errors.Is(err, errWorkspacePackageConflict) {
			writeError(w, http.StatusConflict, err.Error())
		} else if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "resource name or plugin version already exists; the import was rolled back")
		} else {
			var pluginErr *service.PluginError
			var pgErr *pgconn.PgError
			switch {
			case errors.As(err, &pluginErr):
				writePluginError(w, err, "workspace import failed")
			case errors.As(err, &pgErr) && (pgErr.Code == "23514" || pgErr.Code == "23503"):
				writeError(w, http.StatusBadRequest, "invalid resource configuration; the import was rolled back")
			case errors.Is(err, issuestatus.ErrUnknownStatus):
				writeError(w, http.StatusBadRequest, "unknown issue status; the import was rolled back")
			default:
				slog.ErrorContext(r.Context(), "workspace import failed", "workspace_id", uuidToString(ws), "error", err)
				writeError(w, http.StatusInternalServerError, "workspace import failed; the import was rolled back")
			}
		}
		return
	}
	writeJSON(w, http.StatusOK, packageCounts(p))
}
func packageZip(files map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(content); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
func defaultWorkspacePackage() (WorkspacePackage, error) {
	p := emptyWorkspacePackage()
	raw, err := harness.Files.ReadFile("manifest.json")
	if err != nil {
		return p, err
	}
	var manifest struct {
		SystemSkills []string                  `json:"system_skills"`
		Skills       []string                  `json:"skills"`
		Plugins      []string                  `json:"plugins"`
		PluginConfig map[string]map[string]any `json:"plugin_config"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return p, err
	}
	prompt, err := harness.Files.ReadFile("startup.gotmpl")
	if err != nil {
		return p, err
	}
	text := string(prompt)
	p.SystemPrompt = &text
	for i, name := range append(manifest.SystemSkills, manifest.Skills...) {
		directory := "skills/" + name
		content, err := harness.Files.ReadFile(directory + "/SKILL.md")
		if err != nil {
			return p, err
		}
		skillName, description := skillpkg.ParseSkillFrontmatter(string(content))
		s := PackageSkill{ID: name, Name: skillName, Description: description, Content: string(content), Config: map[string]any{}, IsSystemSkill: i < len(manifest.SystemSkills), Files: []CreateSkillFileRequest{}}
		err = fs.WalkDir(harness.Files, directory, func(file string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			relative := strings.TrimPrefix(file, directory+"/")
			if relative == "SKILL.md" || strings.Contains(relative, "__pycache__") || strings.HasSuffix(relative, ".pyc") {
				return nil
			}
			data, err := harness.Files.ReadFile(file)
			if err != nil {
				return err
			}
			s.Files = append(s.Files, CreateSkillFileRequest{Path: relative, Content: string(data)})
			return nil
		})
		if err != nil {
			return p, err
		}
		p.Skills = append(p.Skills, s)
	}
	runtime, err := harness.Files.ReadFile("plugins/runtime.py")
	if err != nil {
		return p, err
	}
	for _, name := range manifest.Plugins {
		raw, err := harness.Files.ReadFile(path.Join("plugins", name, "multica.plugin.json"))
		if err != nil {
			return p, err
		}
		var metadata struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return p, err
		}
		hook, err := harness.Files.ReadFile(path.Join("plugins", name, "hook.py"))
		if err != nil {
			return p, err
		}
		bundle, err := packageZip(map[string][]byte{"multica.plugin.json": raw, "hook.py": append(append(append([]byte{}, runtime...), '\n'), hook...)})
		if err != nil {
			return p, err
		}
		p.Plugins = append(p.Plugins, PackagePlugin{ID: name, Key: metadata.Key, Name: metadata.Name, Bundle: bundle, Config: manifest.PluginConfig[name], Enabled: true})
	}
	return p, nil
}
