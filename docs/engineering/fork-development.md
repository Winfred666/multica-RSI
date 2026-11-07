# Multica-RSI development reference

Implementation differences from [Multica](https://github.com/multica-ai/multica)
for agents and contributors. Keep this reference current when changing fork
behavior. The [README](../../README.md) presents the research platform.

## Backend

- **Editable startup prompts.** We replaced embedded workflow instructions with
  workspace Go templates and SOP skills. You can edit the workspace prompt;
  the server validates the template on save. The kit inlines basic issue workflow
  and CLI rules, removes `multica-issue-turn`, and ends with explicit Never-HERO
  scope limits and Stop-slop writing rules: effect first, plain explanation next, artifact
  or path last. Claims include the issue body alongside its current
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
contains the system prompt, system and library skills, and plugins for parent
wakeups, in-progress continuation and issue-comment cleanup. Workspace creation
installs the embedded kit through the native Go package importer in the workspace
creation transaction. The package API also supports selective export/import of
projects/issues, agents, skills, MCPs, plugins and the system prompt. Imported
agents are offline and require runtime setup; plugin installation never creates
an execution PAT. See [the API contract](workspace-packages.md). Child dispatch uses
`multica-subissue-distribute`; delivery and blockers travel through native replies
and parent steering. Parent review integrates verified work in its own turn.
Quick-create keeps the upstream task routing: its execution rules render only
for a quick-create turn, and the native task prompt supplies picker and field
rules. The kit does not install a global quick-create skill.
See [SOP handoffs](../PLAN.md#sop-calls-and-cross-issue-handoffs).

## Frontend

- **Issue tree.** You can inspect parent/child issues, expand collapsed stacks,
  and preview collaborators' messages and runs, with context-usage rings,
  compression counts and saved pan/zoom. Issue-owned sub-workspaces appear as
  static snapshots with their collaborators and links. Branches use measured
  card heights; root trees wrap at a captured canvas width, refreshed on uncached
  load and issue creation/deletion. Resizing preserves packing; restored cameras
  stay within tree bounds. Cards and artifact thumbnails can be selected
  separately; Delete removes all versions of selected artifacts. Uploaded HTML
  deliverables render in tree cards and open in the artifact viewer.
- **Settings.** You can manage hosted plugin execution credentials and personal
  sub-issue following from workspace settings.
- **Onboarding.** We removed the acquisition-source survey and its follow-up popup.

## CLI and daemon

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
