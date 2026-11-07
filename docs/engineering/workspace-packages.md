# Workspace packages

All routes below are under `/api/workspaces/{id}` and reuse the normal backend
`middleware.Auth`: the existing browser JWT session cookie (with CSRF validation
for state-changing requests), or bearer JWT/PAT authentication. A signed-in browser
does not need a PAT. The existing `RequireWorkspaceRoleFromURL` middleware enforces
owner/admin membership, as it does for workspace settings. Agent task and cloud-node
credentials are excluded using the existing actor classification.

| Method | Path | Request | Response |
| --- | --- | --- | --- |
| GET | `/package` | | Inventory counts; optional summary page |
| GET | `/package/default` | | Full embedded startup package |
| POST | `/export` | Selection | Selected content as a downloadable JSON package |
| POST | `/import/inspect` | `{package, selection?}` | Validated counts; optional summary page |
| POST | `/import` | `{package, selection?, replace_existing?: false}` | Imported counts |

Inventory and inspect return `{counts: {projects, issues, agents, skills, mcps,
plugins, system_prompt}}`. Counts are integers; `system_prompt` is a boolean.
Projects/issues stay a count selection, with no item expansion. To expand another
category, use `?category=agents|skills|mcps|plugins&limit=50&offset=0` on the same
inventory or inspect route. Responses add `category`, `limit`, `offset` (when
nonzero) and `items: [{id, name, is_system_skill?}]` when nonempty. The maximum
limit is 100; category counts give the total. Inventory orders by name and ID;
inspection follows package order. Neither response includes bodies, files,
configurations, ZIPs or issue IDs. The existing resource endpoints provide details.
Import returns the same count fields directly, without ID maps.

A selection is `{projects_issues: false, agents: [], skills: [], mcps: [],
plugins: [], system_prompt: false}`. Arrays contain IDs from inventory summaries
or the uploaded package. Export reads content only for selected categories/IDs;
selecting projects/issues exports the complete group. Omitted export fields
select nothing. Omitted import selection selects everything; `{}` selects
nothing. Unknown IDs fail with 400. Inspect and import accept one JSON request
up to 64 MiB, including base64 data.

There is one package format: `schema_version: 1`, `projects`, `issues`,
`issue_statuses`, `agents`, `skills`, `mcps`, `plugins`, `system_prompt: string|null`.
IDs inside a package link its resources; imports allocate destination identities.
The [Go types](../../server/internal/handler/workspace_package.go) define fields.
Skills include content, config, system classification and supporting
`files: [{path, content}]`; MCPs include config. Plugins include name/key, base64
ZIP `bundle`, non-secret config and enabled state. Statuses include only key,
name, description, category, color and icon.

Skills, MCPs and installed plugins that already exist by name/key return 409
unless the owner/admin supplies `replace_existing: true`. Replacement keeps skill
identity, bindings and system classification; a classification mismatch always
returns 409. Agent name collisions return 409. Plugin versions remain immutable:
the importer reuses matching published content or publishes a new version through
the plugin service. Existing version IDs and digests remain unchanged. When JSONB
has reordered manifest keys, the importer compares only the selected version's
manifest and stored file hashes. Changed content requires a different version;
replacement consent cannot overwrite published code. Plugin selection requires
the server feature flag. Package plugin selection grants the bundle's declared
scopes; execution credentials still require separate setup.

Projects/issues receive new IDs, issue numbers and creators. Hierarchy, selected
agent assignments, dates, stages, metadata and status keys survive. Repository
resource refs use the normal validator and stored normalization. Machine-bound
`local_directory` resources and nonempty issue properties return 400 before any
writes; configure destination resources/property definitions separately.
Comments, attachments, workflows, task/run history and member assignments are
outside the package.

Imported agents are offline, private, owned by the importer and have no runtime
binding, runtime config, environment credentials or invocation grants. Selected
skill/MCP bindings retain enabled state; unselected bindings are omitted.
Destination system skills retain their normal inheritance. Packages can contain
private instructions and MCP configuration. Plugin secrets and execution PATs
are excluded.

Every import commits or rolls back as one transaction and enqueues no agent work.
`POST /api/workspaces` accepts an optional `skip_startup_kit` boolean. Omitted or
false uses this same native importer with the embedded startup kit inside the
creation transaction, preserving an explicitly supplied prompt. True skips only
the embedded kit; owner membership, built-in issue statuses and explicit context
are still created transactionally. Without explicit context, the prompt stays
unset. Startup can then fetch `/package/default` and import its checklist selection
before runtime setup. Creation runs no Python installer, HTTP self-call or PAT
provisioning.
