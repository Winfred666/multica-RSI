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
| POST | `/import` | `{package, selection?, plugin_execution_token?: string}` | Imported counts |

Inventory and inspect return `{counts: {projects, issues, agents, skills, mcps,
plugins, system_prompt}}`. Counts are integers; `system_prompt` is a boolean.
To page through a category, use
`?category=projects|issues|agents|skills|mcps|plugins&limit=50&offset=0` on the same
inventory or inspect route. Responses add `category`, `limit`, `offset` (when
nonzero) and `items: [{id, name, is_system_skill?, key?}]` when nonempty. The maximum
limit is 100; category counts give the total. Inventory orders by name and ID;
inspection follows package order. Neither response includes bodies, files,
configurations or ZIPs. Project and issue summaries use the title as `name`. The existing resource endpoints provide details.
Import returns the same count fields directly, without ID maps.

A selection is `{projects: [], issues: [], agents: [], skills: [], mcps: [],
plugins: [], system_prompt: false}`. Arrays contain IDs from inventory summaries
or the uploaded package. Export reads content only for selected categories/IDs;
projects and issues use exact ID arrays like skills. Omitted, null or empty
export arrays select nothing. A project does not select its issues; an issue
does not select its parent, children, project or agent. Select-all clients pass
all desired IDs, not a boolean. References to unselected resources are removed
from the package, including parent-relative stage when a child is detached.
Source records are unchanged. Omitted import selection selects everything; `{}` selects
nothing. Unknown IDs fail with 400. Inspect and import accept one JSON request
up to 64 MiB, including base64 data.

Issue export uses one ID-filtered issue read and one status read. Project export
uses one project read and one resource batch. Relationship pruning adds no queries.

There is one package format: `schema_version: 1`, `projects`, `issues`,
`issue_statuses`, `agents`, `skills`, `mcps`, `plugins`, `system_prompt: string|null`.
IDs inside a package link its resources; imports allocate destination identities.
The [Go types](../../server/internal/handler/workspace_package.go) define fields.
Skills include content, config, system classification and supporting
`files: [{path, content}]`; MCPs include config. Plugins include name/key, base64
ZIP `bundle`, non-secret config and enabled state. Statuses include only key,
name, description, category, color and icon.

Skills and MCPs with matching names, and plugins with matching keys, are replaced
within the destination workspace. The checklist marks matching items; the CLI
prints a short replacement hint. Inventory includes plugin keys for this comparison. Replacement keeps skill
identity, bindings and system classification; a classification mismatch always
returns 409. Agent name collisions return 409. Plugin versions remain immutable:
the importer reuses matching published content or publishes a new version through
the plugin service. Existing version IDs and digests remain unchanged. When JSONB
has reordered manifest keys, the importer compares only the selected version's
manifest and stored file hashes. Changed content requires a different version;
import cannot overwrite published code. Plugin selection requires
the server feature flag. Package plugin selection grants the bundle's declared
scopes. Supplying `plugin_execution_token` activates hosted execution with that
PAT through the existing activation service, in the same import transaction.
Omitting it leaves the workspace's execution credential unchanged. Activation
requires the plugins feature flag, host configuration and a valid member PAT;
the credential is encrypted and excluded from the package and response.

Projects/issues receive new IDs, issue numbers and creators. Selected hierarchy, selected
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
`POST /api/workspaces` creates owner membership, built-in issue statuses and any
explicit context transactionally. It installs no startup resources. Onboarding
fetches `/package/default` and imports the checklist selection before runtime
setup; leaving every resource box unchecked installs nothing. Settings → General
→ Danger Zone → Restore / import harness opens the same import module. Export
stays in the package section. Both flows start with the default kit; an uploaded export
replaces the selected source. Reading or inspecting a file writes no resources.
The browser reads JSON with `File.text()`, checks the package schema and calls
`/import/inspect`; clicking Import posts the package and checklist selection to
`/import`. There is no separate upload store or server-side file path.

The default route builds a package from the server's embedded `harness/manifest.json`,
`startup.gotmpl`, skills and plugin bundles; it reflects the deployed server build.
For clients with a member PAT, activation defaults on during startup and off for
existing workspaces. Clients reuse their current PAT when activation is selected;
otherwise existing workspace authorization is used implicitly. Import has no PAT input.

## CLI

`workspace package` returns counts. Page through a category to find
IDs, then pass the backend selection object through `--selection-file`:

```sh
multica workspace package SOURCE
multica workspace package SOURCE --category skills --limit 50 --offset 0
# selection.json: {"projects":["PROJECT_ID"],"issues":["ISSUE_ID"],"skills":["SKILL_ID"]}
multica workspace export SOURCE --selection-file selection.json --file workspace.json
multica workspace import DESTINATION --file workspace.json --inspect
multica workspace import DESTINATION --file workspace.json --inspect --category skills
multica workspace import DESTINATION --file workspace.json
multica workspace import DESTINATION --file workspace.json --activate-plugins
multica workspace import DESTINATION --file workspace.json --selection-file selection.json
multica workspace package DESTINATION --default > startup.json
```

The optional workspace accepts an ID, slug or short ID prefix and otherwise uses
the current profile's workspace. Export selects nothing when no selection file
is supplied; import selects everything when it is omitted and nothing for `{}`.
Import selection IDs come from the package, not destination resources. Matching
skill/MCP names and plugin keys in this workspace are replaced automatically.
Imported agents remain offline and require runtime setup. Export writes
JSON to stdout unless `--file` is supplied; new package files use mode `0600`.

`--activate-plugins` defaults to false. Set it to activate hosted execution using
the member PAT authenticating the CLI; JWT and agent task credentials cannot be
reused for activation. Normal import prints the current setting and opt-in syntax
to stderr, keeping JSON stdout unchanged. `--inspect` never activates execution.
