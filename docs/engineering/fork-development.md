# Multica-RSI development reference

Implementation differences from [Multica](https://github.com/multica-ai/multica)
for agents and contributors. Keep this reference current when changing fork
behavior. The [README](../../README.md) presents the research platform.

## Backend

- **Editable startup prompts.** We replaced embedded workflow instructions with
  workspace Go templates and SOP skills. You can edit the workspace prompt;
  the server validates the template on save. The kit routes issue turns to the
  brief textual `multica-issue-workflow` skill only when available. Its textual
  reference owns label-specific delivery comments, acceptance and benchmark
  completion rules; masking the skill removes them. Other skills and continuation
  prompts defer to it for delivery and review, keeping their own API, delegation,
  blocker and resource contracts. The prompt keeps common CLI rules,
  and ends with explicit Never-HERO
  scope limits and Stop-slop writing rules: confirmed verdict/achievement first, cause or method and decisive
  evidence next, with useful references beside each claim. Visuals show cause and effect
  when clearer than prose. Artifacts default off and, when useful, must be visual HTML or images unless explicitly requested otherwise. Claims include the issue
  body alongside its current
  title/status/assignee, so updated daemons supply the assignment in the per-turn
  prompt without requiring a startup CLI read. Existing daemons ignore the new
  optional field; an older server still requires the explicit read. Child prompts
  use their own issue and explicit handoff context; the daemon does not add parent-reading guidance.
- **Configurable subscriptions and wakeups.** You can control inherited sub-issue
  following per user and workspace. We retired the built-in child-done wakeup
  and moved parent wakeup policy into plugins. You can create explicit waits
  through the existing wakeup API.
- **Hosted plugins with PAT credentials.** Workspace owners and admins can supply
  a personal access token for packaged hooks that execute on the server. We
  added encrypted credential storage and execution controls. Hooks run as the
  PAT owner with unsandboxed host access.
- **Concurrency.** We raised the default agent limit from 6 to 50. We changed
  batch claims to fill available slots with multiple tasks per agent and notify
  the runtime when an admin raises an agent's limit.

See the [hosted plugin guide](hosted-workflow-plugins.md) for
configuration and API details. The [workspace startup kit](../../harness/README.md)
contains the system prompt, system and library skills, and two plugins for
in-progress continuation and issue-comment cleanup. Optional skills are SOP
authoring and harness backup/restore; Mika's
onboarding essentials live in the chat system prompt. Workspace creation
installs no kit. Onboarding imports the selected resources through the shared
checklist. The package API also supports selective export/import of
projects/issues, agents, skills, MCPs, plugins and the system prompt. Imported
agents are offline and require runtime setup; plugin installation never creates
an execution PAT. See [the API contract](workspace-packages.md). Child dispatch uses
`multica-subissue-distribute`; delivery uses the textual issue-workflow skill and native
comments, with `in_review` awaiting acceptance. Parents accept children; bounded
benchmarks follow their scoped completion policy. Other root completion stays human.
The issue workflow keeps progress and final delivery in one task thread with
`--parent`; corrections to another agent's active work use `--steer` when its run
accepts input. Thread placement alone does not deliver input into a running turn.
The kit has no separate executable delivery or review procedure.
Quick-create keeps the upstream task routing: its execution rules render only
for a quick-create turn, and the native task prompt supplies picker and field
rules. The kit does not install a global quick-create skill.
See [SOP handoffs](../PLAN.md#sop-calls-and-cross-issue-handoffs).

## Frontend

- **Issue tree.** You can inspect parent/child issues, expand collapsed stacks,
  and preview collaborators' messages and runs, with context-usage rings,
  compression counts and saved pan/zoom. Issue-owned sub-workspaces appear as
  static snapshots with their collaborators and links. Branches use measured
  card heights; root trees wrap at the canvas width, captured on every load
  and issue creation/deletion, never restored from cache. Resizing preserves
  packing; new views start at 100%, and saved cameras stay within tree bounds.
  Cards and artifact thumbnails share the bottom selection toolbar, with
  separate issue and deliverable counts and actions. Deleting deliverables
  removes all their versions without deleting comments. Uploaded HTML
  deliverables render in tree cards and open in the artifact viewer. Shared HTML
  previews allow scripts and file downloads, but keep an opaque origin; parent
  access, forms, popups and top-level navigation remain sandboxed.
- **Settings.** You can manage hosted plugin execution credentials and personal
  sub-issue following from workspace settings. Owners/admins can import and export
  workspace packages with selective checklists; resource names load in bounded pages.
  Project/issue selectors are exact ID arrays, default off for export, with no
  relationship expansion. References outside the package are detached in memory.
- **Onboarding.** We removed the acquisition-source survey and its follow-up popup.
  New workspaces defer the startup kit to the shared import checklist before runtime
  setup. Import selects all resources; export selects none. Startup import can unselect
  all resources and warns in red when the system prompt, skills or plugins are omitted.
  Both startup and Danger Zone imports use the default kit or an uploaded export.
  Hosted plugin activation reuses a client member PAT (on for startup, off for existing
  workspaces); session users manage credentials in Settings → Plugins.

## CLI and daemon

- **Workspace operations.** Workspace commands require a member context. Ordinary
  daemon tasks and task tokens are refused with a plain permission error.
  The local daemon has no workspace PAT
  proxy. Task credentials stay bound to their original workspace; backend APIs
  and membership checks are unchanged. Member plugin activation is opt-in and
  uses only the member PAT authenticating the CLI.
  `workspace create --import-file package.json` reuses import and `--selection-file`;
  it prints the created workspace even if import fails, so import can be retried.

- **Remote SOPs.** `multica remote-sop ISSUE SOP [args...]` posts
  `/note !sop <name> <JSON argv>`. Try `--help` as the first SOP argument.
  The native `/note` prefix prevents agent activation. On each heartbeat the
  daemon takes these notes for its runtime, moves them into execution logs,
  and runs `python3 <skill>/wrapper.py` directly with task ID `0`.
  Bound skill files and workspace `.agents/` folders supply SOPs containing
  `wrapper.py`. Argument boundaries, combined stdout/stderr and
  exit codes are preserved; missing names return the available SOP list.
  Calls never use agent task admission, steering, provider startup or agent
  environment preparation. Completed execution records hold only the request
  and output. Registration and issue metadata writes are unnecessary.

- **Symbolic SoP protocol changes.** Update `harness/skills/create-symbolic-sop`
  and its template first; require human approval before mechanically propagating
  the approved protocol to other symbolic SoPs and distributions. The approved
  template derives its graph from `controller.workflow` without `DEBUG.md`, shows
  agent JSON and rendered DOT logic from action docstrings before the final
  issue/SoP metadata. Workflow states, option targets and explicit Branch routes
  supply the global graph; action docstrings supply only local logic. Graphs fit the
  viewport width in landscape and scroll horizontally at natural size in portrait.
  Graphviz `dot -Tsvg` compiles all DOT diagrams to embedded SVG on the CLI host;
  previews have no external renderer or network dependency. The wrapper's
  `ensure_command` reuses tools on PATH or installs their declared OS package
  when missing; Graphviz and bubblewrap use it. Failed installation returns an
  actionable wrapper exception. Help does not install dependencies.
  Native small-circle start and double-circle stop markers have non-clickable boundary links. The creator's
  `scripts/check.py` validates a selected package and checks its shared runtime in
  temporary template fixtures. CLI `--help` replays a recorded decision or shows
  the skill body without starting a run. The R-262 canonical template stores serialized
  JSON in one `sop.<skill-name>` issue metadata key, with no local task state file.
  It resumes across tasks/agents, rebinds replayed commands to the current checkout,
  and deletes only its own key on completion or quit. Required identity is issue
  and workspace. Each key keeps only the current status, latest guidance and necessary
  procedure facts, with no decision history, diagram cache or local error outbox.
  Executable system SoPs use the same approved issue-owned protocol. The refinement
  template uses the current issue folder for tests and logs, with no saved workspace path.
  The existing scalar API and 8 KB issue metadata
  limit apply; overlapping callbacks on the same SoP require coordination.
  Controller `data` remains internal; state instructions and option descriptions
  expose only selected facts in CLI, help and HTML.
  The blocker SoP marks the issue blocked and steers one concise help request
  to its parent's original assignment thread, or a new parent thread if none
  exists. It posts no separate blocker note or return receipt on the current
  issue; the agent's normal reply explains the blocker.

- **Workspace packages.** `workspace package`, `workspace export` and
  `workspace import` expose the native package API with paged summaries, JSON
  selection files and inactive imported agents. See [usage and defaults](workspace-packages.md#cli).
  Import and create-with-import can activate hosted plugins with `--activate-plugins`
  (default false); stderr reports the setting. Human commands use the current member
  PAT; for agent tasks the daemon injects its PAT without returning it to the CLI.

- **Plugin commands.** You can inspect execution credentials, enable or disable
  hosted execution, and configure installed plugins:

  ```sh
  multica workspace plugin-execution status WORKSPACE
  multica workspace plugin-execution enable WORKSPACE --token-stdin < /path/to/pat
  multica workspace plugin-execution disable WORKSPACE
  multica workspace plugin list WORKSPACE
  multica workspace plugin configure INSTALLATION_ID WORKSPACE --config-file prompts.json
  ```

- **Startup prompts.** We render the workspace template into each provider's
  context file and expose the issue ID through `MULTICA_ISSUE_ID`.
- **Context measurements.** We collect native context-token measurements in the
  provider adapters and write them to issue metadata for the tree indicators.
- **Execution slots.** We raised the daemon default from 20 to 50 and made it
  continue claiming work after a partial batch. New agents created through the
  CLI default to 50 concurrent tasks; use `--max-concurrent-tasks` to set a limit.

- **Watchdog budgets.** Silence means no new runtime protocol messages, including
  while an SOP waits inside a tool call. The default idle and tool budgets are
  24 hours; Codex derives its semantic inactivity budget from the larger of them.
  Saved CLI profile settings remain editable and take effect on daemon restart.
  Explicit provider timers and `agent_timeout` can impose shorter limits.
  A watchdog stop records a failed task and an issue system comment. See
  [runtime configuration](../../CLI_AND_DAEMON.md#configuration) for overrides,
  disable behavior and failure reporting.

## Distribution

GitHub Releases provides the CLI and daemon binaries. The Bash and PowerShell
installers, in-app install commands, and CLI update checks use this fork.
Self-host Compose pulls backend/web images from `ghcr.io/winfred666`.
Use the [self-hosting installer](../../SELF_HOSTING.md#quick-install-recommended);
users do not need to build the images.

## Development

Build this checkout using the [contributing guide](../../CONTRIBUTING.md).
See [self-hosting](../../SELF_HOSTING.md) for deployment and
[CLI and daemon setup](../../CLI_AND_DAEMON.md) for runtime configuration.

## License and attribution

See the [Multica License](../../LICENSE) and [NOTICE](../../NOTICE) for terms and upstream
attribution. Index Labs develops the original
[Multica repository](https://github.com/multica-ai/multica).

## Independent deliverables (Backend P1)

Independent deliverables reuse attachment rows and storage, with an explicit
`independent` boolean, default false. They have no issue, comment, task, chat or
source-context owner. Existing uploads are not converted. Uploader fields record
provenance only. Ordinary attachment operations keep their existing behavior.

Use these existing endpoints with the resource's workspace context:

- Create: `POST /api/upload-file`, multipart `file` and `independent=true`.
  All owner fields must be absent. Response is the existing attachment shape with
  `independent:true`. A new upload without an ID creates a new resource even when
  its filename matches another resource.
- Discover, including unlinked resources:
  `GET /api/workspaces/{workspaceUUID}?include=deliverables` adds
  `deliverables: AttachmentResponse[]`. Without this opt-in the response is
  unchanged. Discovery includes all independent resources in that workspace,
  regardless of issue links; ordinary uploads are excluded.
- Read: `GET /api/attachments/{id}`; preview and download retain the existing
  `/{id}/content` and `/{id}/download` routes. Persist `markdown_url`, which always
  follows the stable ID download route for independent resources. Independent
  downloads use the existing no-store proxy, including signed download
  capabilities, so storage/CDN caching cannot serve prior content.
- Replace: `POST /api/upload-file`, multipart `file`, `independent=true` and
  `attachment_id={uuid}`. Only an existing independent object in the selected
  workspace is eligible. The resource row is locked while current bytes overwrite
  its existing storage key and file metadata is updated. No previous blob,
  backup, or content-history row is created. Storage must not retain object
  versions externally if no-history storage is required.
- Link: `PUT /api/issues/{issue}/metadata/deliverables`, with
  `{"value":"[\"<independent-uuid>\"]"}`. The `deliverables` value is a primitive
  **string encoding a JSON array** of distinct canonical UUIDs. The server checks
  that every ID is an independent resource in the issue's workspace. Share the
  same ID by recording it on multiple issues. Read through the existing issue
  metadata API or the issue's `metadata` response.
- Unlink: write the remaining array (encoded `[]` is valid), or
  `DELETE /api/issues/{issue}/metadata/deliverables` to remove all links. These
  operations only change metadata. Deleting an issue likewise removes only its
  links; resources and other issues' links survive. Existing metadata limits
  (50 keys, 8KB) and single-key last-writer semantics apply. Clients updating a
  link set must read/modify/write the entire encoded array.

Clients should omit missing resources when rendering stored links. Explicit
resource deletion still uses the existing attachment deletion endpoint and is
separate from unlinking; it does not rewrite issue metadata. The Frontend and CLI
slices own their client/schema changes. Plan: [P1 #50](https://github.com/Winfred666/multica-RSI/issues/50).

### Independent deliverables in web and desktop

The shared issue tree discovers workspace resources through `?include=deliverables`.
Only explicit `independent:true` objects become free root nodes, one per ID,
including unlinked objects. Same-name independent IDs are never filename versions.
Issue cards and the detail sidebar show **Linked deliverables** separately from
comment outputs; dashed tree connectors represent the metadata relationship.
Missing resources are omitted. Removing links or issues leaves free nodes intact.
The existing attachment viewer opens both surfaces. While these views are open,
workspace discovery refreshes every 30 seconds and invalidates independent preview
caches so replacement serves current bytes. Ordinary comment outputs retain their
existing grouping and version history.


## Independent deliverables (SoP template)

The canonical creator/template publishes live HTML and DEBUG previews with
explicit independent IDs. `sop_artifacts` retains each skill/filename slot's UUID;
`deliverables` links the resource to the issue. Both metadata values are encoded
strings and survive transient SoP state cleanup. Repeated callbacks replace by ID,
not filename; ordinary comment-bound uploads stay unchanged. Publication errors
leave workflow results intact and require reconciliation before retrying.

The [publication contract](../../harness/skills/create-symbolic-sop/references/independent-publication.md)
defines create/read/replace/link/unlink and exact CLI spelling for the CLI slice:
`attachment upload FILE --independent [--attachment-id ID]`,
`attachment list --independent`, `attachment get ID`, existing `attachment download`
and read/modify/write via `issue metadata set --key deliverables --type string`.
These new CLI flags/commands await CLI implementation. This template change is
not a rollout: human approval is required before propagating to other symbolic
SoPs or installed distributions.
