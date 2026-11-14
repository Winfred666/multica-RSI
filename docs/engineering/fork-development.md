# Multica-RSI development reference

Implementation differences from [Multica](https://github.com/multica-ai/multica)
for agents and contributors. Keep this reference current when changing fork
behavior. The [README](../../README.md) presents the research platform.

## Backend

- **Renewable attachments.** `POST /api/upload-file` accepts multipart `file`
  alone to create an ownerless workspace attachment, or `file` plus
  `attachment_id` (UUID) to renew that ownerless row. Renewal preserves its ID,
  replaces its current file and deletes the previous object; it keeps no versions
  or backups. Do not send owner fields on renewal. Issue/comment-owned and chat
  files cannot be renewed this way. Task-upload replacement skips comment-owned
  files, preserving dependent comment history.
  `POST /api/attachments/{id}/attach` and `/detach` both take JSON
  `{"issue_id":"UUID"}`. Attach claims an ownerless file for that issue; an
  existing issue-only attachment to the same issue is a no-op. Detach requires
  that issue to be the current owner and clears both `issue_id` and `comment_id`.
  Neither operation changes file ID or bytes. All three operations return HTTP
  200 with the existing `AttachmentResponse`, including the same `id` and current
  nullable owner fields. Use authentication and `X-Workspace-ID` (or slug).
  Mutations require the uploader actor or a workspace owner/admin. A conflicting
  owner returns 409, unauthorized mutations 403, missing resources 404 and invalid
  UUIDs/payloads 400. Ordinary dependent deletion is unchanged.
- **Editable startup prompts.** We replaced embedded workflow instructions with
  workspace Go templates and SOP skills. You can edit the workspace prompt;
  the server validates the template on save. The kit routes issue turns to the
  brief textual `multica-issue-workflow` skill only when available. Its textual
  reference owns label-specific final answers, acceptance and benchmark
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
`multica-subissue-distribute`; agents answer naturally and completion publishes
the reply, with `in_review` awaiting acceptance. Parents accept children; bounded
benchmarks follow their scoped completion policy. Other root completion stays human.
The issue workflow keeps deliberate comments in one task thread with
`--parent`; corrections to another agent's active work use `--steer` when its run
accepts input. Thread placement alone does not deliver input into a running turn.
The kit has no separate executable delivery or review procedure.
Quick-create keeps the upstream task routing: its execution rules render only
for a quick-create turn, and the native task prompt supplies picker and field
rules. The kit does not install a global quick-create skill.
See [SOP handoffs](../PLAN.md#sop-calls-and-cross-issue-handoffs).

## Frontend

- **Soft-linked deliverables.** The issue's `independent_artifacts` metadata is a
  JSON-string attachment ID list. Existing attachment reads resolve unique IDs;
  ownerless files appear before dependent deliverables in the tree and existing
  Deliverables sidebar, using the same previews. Task provenance is not ownership;
  issue, comment and private-chat owners exclude a soft target. Text links do not
  define this relationship. Independent IDs stay separate even with matching names;
  ordinary comment versions remain grouped as before.

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

- **Attachment commands.** `attachment upload PATH` creates an ownerless file,
  ignoring ambient issue/task ownership; `--attachment-id UUID` renews it using
  the same server-returned ID. `--task TASK_UUID` explicitly keeps chat-reply
  upload behavior and cannot be combined with renewal. `attachment get ID` and
  `attachment download ID` read the current file.
  `attachment attach ID --issue KEY_OR_UUID` and `attachment detach ID --issue
  KEY_OR_UUID` change actual issue ownership. Add `--soft` to validate that the
  file is ownerless and only update the issue's `independent_artifacts` metadata
  as a JSON-string ID list, deduplicating IDs and preserving other entries and
  metadata. Neither detach mode deletes the file; the server owns history policy.

- **Workspace operations.** Workspace commands require a member context. Ordinary
  daemon tasks and task tokens are refused with a plain permission error.
  The local daemon has no workspace PAT
  proxy. Task credentials stay bound to their original workspace; backend APIs
  and membership checks are unchanged. Member plugin activation is opt-in and
  uses only the member PAT authenticating the CLI.
  `workspace create --import-file package.json` reuses import and `--selection-file`;
  it prints the created workspace even if import fails, so import can be retried.

- **CLI-owned symbolic SoPs.** `multica sop list` prints only names and source
  paths. `multica sop run NAME [args...]` resolves and runs the Python engine
  embedded in the CLI binary, preserving argument boundaries, cwd and exit codes.
  Skills declare `metadata: {sop: symbolic}` in `SKILL.md` frontmatter and supply
  `controller.py`; discovery never imports controllers. `config.json` and its
  `DEBUG` key are optional; DEBUG defaults to false. No config file is generated.
  Injected `MULTICA_SOP_SKILL_ROOTS` (an OS path-list), `CODEX_HOME/skills`,
  `HERMES_HOME/skills` and `CLAUDE_CONFIG_DIR/skills` supply installed skills.
  The calling repository's skill roots, including `.agents/skills` and
  `harness/skills`, override installed copies by frontmatter name, even from a
  repository subdirectory. The engine receives separate semantic and writable
  state directories as `context['sop_dir']` and `context['state_dir']`; transient
  workflow state remains in issue metadata. Callbacks use `multica sop run`,
  not copied wrappers. `--help` is passed to the selected SoP unchanged.
  An option action's `this_round_reply_guidance` replaces the invocation's
  instruction and omits all top-level next steps. Stored guidance, ownership and
  transitions remain intact for later resume/help. Use `multica sop docs` for
  authoring guidance. This adds no provider-stop mechanism.
  The daemon executes remote `/note !sop <name> <JSON argv>` requests through the
  same CLI in the target working directory with scoped credentials. Execution
  is bound to issue/workspace context and requires no task ID.
  The obsolete `remote-sop` CLI command and wrapper-presence resolver are removed.

- **SoP developer kit.** `multica sop docs` is the canonical authoring guide;
  `multica sop dev docs` prints the same guide for compatibility. `multica sop dev init DIRECTORY`
  accepts an empty or nonexistent directory with `--minimal` (default) or
  `--feature-rich`; both write a semantic controller, `SKILL.md` and a searchable
  `DEVELOPMENT.md`. `multica sop dev validate DIRECTORY` reuses
  the resolver's metadata reader and the engine's Workflow validation without
  issue/task context. Ordinary `sop list`, `sop run` and procedure help are unchanged.

- **Engine publication.** The CLI-owned engine publishes shared config JSON
  and DEBUG-compiled HTML with ownerless CLI upload/renew and soft-attach to the
  current issue. IDs persist in the CLI-managed `independent_artifacts` list;
  controller-supplied ordinary comment attachments and DEBUG triggers are unchanged.

- **Registered option actions.** The CLI Workflow accepts reusable `Option` nodes
  and `Branch(select=...)` routes through options/diamonds until an interactive
  State. HTML renders those same nodes once without running their logic. The
  validator requires automatic nodes to have a path to a State or `FINISHED`,
  while allowing State-to-State loops. Consecutive actions recurse with a default
  limit of 5, configurable through `config set sop_max_action_depth`; selected
  diamond-only loops are rejected. Rollout remains subject to human approval.

- **Symbolic SoP protocol changes.** Update the CLI-owned engine and
  `server/internal/cli/sopdev/DEVELOPMENT.md`, served by `multica sop docs`, not
  the obsolete authoring skill. Require human approval before mechanically
  propagating the approved protocol to other symbolic SoPs and distributions. The
  CLI runtime derives its graph from `controller.workflow` without `DEBUG.md`, shows
  agent JSON and rendered DOT logic from action docstrings before the final
  issue/SoP metadata. Workflow states, option targets and explicit Branch routes
  supply the global graph; action docstrings supply only local logic. Graphs fit the
  viewport width in landscape and scroll horizontally at natural size in portrait.
  Graphviz `dot -Tsvg` compiles all DOT diagrams to embedded SVG on the CLI host;
  previews have no external renderer or network dependency. The wrapper's
  `ensure_command` reuses tools on PATH or installs their declared OS package
  when missing; Graphviz and bubblewrap use it. Failed installation returns an
  actionable wrapper exception. Help does not install dependencies.
  Native small-circle start and double-circle stop markers have non-clickable boundary links.
  `multica sop dev validate DIRECTORY` checks a selected package with the CLI-owned engine.
  CLI `--help` replays a recorded decision or shows
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

- **Built-in symbolic SoPs.** The blocker and child dispatch skills keep their
  instructions, controllers and domain helpers, not copied runtime modules.
  They use CLI callbacks with their existing checks, side effects and issue-owned
  `sop.<name>` state. Help replays guidance without executing the procedure.
  Protocol changes belong in the engine; propagation must preserve controllers
  and live issue records.
  The blocker SoP marks the issue blocked and steers one concise help request
  to its parent's original assignment thread, or a new parent thread if none
  exists. It posts no separate blocker note or return receipt on the current
  issue; the agent's normal reply explains the blocker.

- **Addressed SoP owners.** A state owner dictionary with `issue_id`, `thread`,
  `id` and native comment `content` is rendered after the option/action chain.
  The ordinary agent comment contains `!sop-render <skill-name> <owner JSON>`
  and the existing redacted, ordinary state JSON, not the masked invocation reply.
  `content` supplies native `/note` or agent-mention syntax; only the three destination
  IDs are encoded, and the execution assignee is unchanged.
  Rendered JSON adds frontend-ignored `how_to_step` text with the ordinary comment-post
  command for `/note !sop-remote <skill-name>` and a questionnaire-comment-ID placeholder;
  the renderer does not post/read back to discover its ID.
  Controller validation checks that the final action on a declared normal-agent
  to addressed-state path returns `this_round_reply_guidance`. Stage 3 owns forwarding
  and rendering; this adds no state/question schema or remote-call dispatch.

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

## Attachment ownership (Backend P1)

Deliverables, description inputs and comment uploads use the existing
`attachment` table. “Input” is a UI interpretation, not a stored type.
`issue_id` and `comment_id` are nullable owning foreign keys with their original
cascade behavior. Comment-bound rows normally retain both the comment and its
parent issue ID. Independence means both owner columns are null; there is no
stored independent flag, separate object/link table or backend soft-link model.

### Existing routes and exact contract

- **Create:** `POST /api/upload-file`, multipart `file`, with workspace context.
  No `independent` flag. A plain upload has neither issue nor comment owner.
  Existing `issue_id`, `comment_id` and task upload bindings remain dependent.
- **Attach ownership:** existing issue create/update `attachment_ids` binds
  unowned workspace files to that issue. Existing comment create/edit
  `attachment_ids` binds unowned or same-issue unbound files to that comment
  and its parent issue. Neither operation moves another owner's file. A comment
  edit replaces its attachment set; omitting `attachment_ids` leaves it alone.
- **Explicit detach:** `PUT /api/issues/{id}` with
  `{"remove_attachment_ids":["attachmentUUID"]}` clears both owner columns for
  selected files owned by that issue or its comments. Removing a file from its
  original comment's `attachment_ids` clears both columns too. These operations
  keep the same file ID and bytes. Attaching and removing the same ID in one
  issue update detaches first, then reattaches it to the issue.
- **Normal deletion:** deleting an issue/comment still deletes its dependent
  attachments and stored bytes. Explicit detachment must happen first to protect
  a file. Deleting an attachment or workspace still deletes resources normally.
- **Issue lookup:** existing `GET /api/issues/{id}/attachments` and issue
  `attachments` projections select rows by owning `issue_id` and workspace.
  Normal comment-bound rows already appear because they retain the parent issue
  ID. A legacy upload supplied only `comment_id` is not included by this query.
  An ownerless file mentioned through a client soft link does not appear there.
- **Workspace lookup:** existing `GET /api/workspaces/{workspaceUUID}` with
  `?include=attachments` includes an `attachments` array of ordinary workspace
  attachment responses, excluding private chat and source snapshots. This
  generalizes the earlier P1 `include=deliverables` projection; it adds no HTTP
  route. Without the opt-in, the response is unchanged. Callers select ownerless
  files by absent `issue_id` AND `comment_id`; there is no floating/soft-link
  search or filter. Each entry carries the existing ID, filename, content type,
  byte size and read/download URLs. This list has the existing unpaginated shape.
- **Read:** existing `/api/attachments/{id}`, `/{id}/content` and
  `/{id}/download` work without an issue/comment owner, with workspace access
  checks unchanged. Persist `markdown_url`. Ownerless files use the stable ID
  route and no-store proxy to avoid immutable caching during renewal; original
  dependent files retain their download/signing behavior.
- **Replace:** existing `POST /api/upload-file`, multipart `file` and
  `attachment_id={uuid}`. Under the owner/row locks, the backend renews
  only ownerless files using the same ID/key without a previous-byte backup.
  Renewal rejects issue/comment-owned files, private chat and source snapshots.
  Task provenance alone is not ownership. The response ID is authoritative;
  clients choose no history mode.
  External object-version retention must be disabled for no-history renewal.

There are **no new routes** or metadata requirements. Metadata, including any
`deliverables` value, is opaque to this feature: no validation, projection,
migration or cleanup. Clients may soft-link the same independent ID from several
issues, but that does not establish ownership or affect history/deletion policy.
Deleting those issues/comments cannot hurt an ownerless attachment. The backend
cannot distinguish a soft-linked file from a completely unreferenced one.

Migration 618 only removes the erroneous `independent` column/check. It creates
no table/index and changes no file IDs, ownership, metadata, history or bytes.
Regenerate query bindings after SQL changes with `make sqlc`.

Plan: [Backend P1 #50](https://github.com/Winfred666/multica-RSI/issues/50).

### CLI

`attachment upload FILE [--attachment-id ID]` uses the ordinary upload route
without a required task or independent mode. Ambient task binding is ignored;
`--task` explicitly selects a new chat upload and cannot be combined with renewal.
The upload retains markdown output; the server's returned ID is authoritative. `attachment get` returns
metadata; `attachment download` saves bytes. Workspace discovery uses
`workspace get --include attachments --output json`. Existing issue create
`--attachment-id` binds uploaded IDs; issue update accepts `--attachment-id`
and `--remove-attachment-id` for the backend's attach/paired-detach operations.
Normal issue/comment `--attachment` paths are unchanged. The `attachment attach`
and `detach` commands provide hard ownership and `--soft` metadata links as
described above. There is no client history mode or independent-only list.

### Common preview tiles

Workspace `?include=attachments` supplies ordinary files. Tree free nodes select rows with both owner fields empty and deduplicate by ID, not filename. They float beside the issue trees on a separate layer beneath the cards, outside root packing, and reuse common preview tiles without extra titles or filename headers. Description content links use the ordinary attachment URL matcher to draw smooth dashed SVG cubic Bézier soft-reference curves without a routing library; they do not establish ownership. Comment files in the FK attachment projection join the existing deliverable collection and version rendering even before their timeline loads. The issue's `independent_artifacts` metadata additionally supplies soft-linked ownerless files in its tree card and Deliverables section.

## SoP configuration artifacts

The CLI-owned engine follows the [R-328 shared wrapper protocol](https://github.com/Winfred666/multica-featssship/tree/c396c654f245e2dd4d0686f70a16a73101ac4188/installation),
including declarative owners, guarded assignee synchronization and FINISHED re-entry.
Its `common.py` helpers retain shared configuration references. The runtime
publishes present config JSON and DEBUG HTML through CLI ownerless upload/renew
and soft-attach commands. MAIN/developer/reviewer
readers share small references; configuration payloads are not issue metadata.
Transient SoP runtime/recovery state remains separate. Shared artifact IDs
live in `independent_artifacts`; controller-supplied artifacts stay ordinary
comment attachments. DEBUG settings and triggers are unchanged.

See `multica sop docs` for the engine-owned authoring contract.
The CLI manages soft-link metadata; no independent upload flag is used.
Further propagation to other SoPs or installs requires human approval.

## Natural-answer issue completion

Daemon issue prompts use the existing provider-neutral `agent.Result.Output` →
`TaskResult.Comment` → terminal `/complete` transport. Successful completion
publishes the final answer without a routine reply file, CLI final-post call, or
second acknowledgement. Assignment answers are top-level; comment-triggered
answers use the current trigger, including resumed sessions and wakeups.
Explicit quiet/no_action outcomes leave the final response empty. Deliberate
comments and attachments remain available; for coalesced cross-thread runs,
each thread receives a deliberate reply and the final response stays empty.
The existing backend suppresses completion output if this agent already commented
on the issue during the run, including progress comments; changing that gate is
outside the daemon scope.
Failed/cancelled delivery and adapter extraction are unchanged.

Contract: [natural-answer completion #40](https://github.com/Winfred666/multica-RSI/issues/40).
