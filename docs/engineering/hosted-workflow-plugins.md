# Hosted workflow plugins: minimal proposal

Status: hosted execution implemented; automatic credentials remain below.

## Decision

Reuse native workflows, wakeups and the plugin scheduler. The core change is
limited to executing a packaged script and supplying its credentials. Workflow
policy ships as independently upgraded ZIPs inside the deployed Multica
container. One initial host release is required; subsequent package changes
need neither manual PAT setup nor another Multica deployment.

## What is already available

A workflow is a reusable board configuration. For example, **Implementation**
hands an issue to a coding agent with implementation instructions; **Review**
hands it to a reviewer; **Acceptance** hands it to the project lead. Entering
a configured column changes the assignee and starts the agent when applicable.
Several projects can select the same workflow, so editing it affects them all.
The instructions guide the agent; they do not themselves verify the result.
See the [workflow guide](../../apps/docs/content/docs/workflows.mdx).

| Existing capability | What the package reuses |
| --- | --- |
| [Project workflows #8814](https://github.com/multica-ai/multica/pull/8814) | Columns, step instructions and handoffs. Read the project's actual statuses and handlers. |
| [Wakeup conditions #8807](https://github.com/multica-ai/multica/pull/8807) | Child completion, supported conditions, timers and ordinary agent runs. |
| [Plugin packages #7321](https://github.com/multica-ai/multica/pull/7321) | ZIP storage, immutable versions, installation, configuration and explicit upgrades. |
| [Scheduled hooks #7448](https://github.com/multica-ai/multica/pull/7448) | Cron scheduling, execution records, retries and lifecycle handling. |

These are merged. Plugin and workflow management remain gated by
`FF_PLUGINS_V1` and `FF_PROJECT_WORKFLOWS_V1`. The latest upstream increment,
[#9061](https://github.com/multica-ai/multica/pull/9061), fixes comment routing;
it adds no hosted plugin executor.

## Hosted execution

The `exec` transport extends the existing
[manifest](../../server/pkg/plugincontract/manifest.go), package file validation
and [hook dispatcher](../../server/internal/service/plugin_hook.go).
It resolves the entry from the installed immutable version and runs it on
Linux/macOS. Declare the `host:exec` scope: the installation preview explains
that this is unsandboxed server execution, beyond ordinary plugin scopes.

Each invocation gets an empty temporary working directory and home, the existing
hook payload and non-secret config as JSON on standard input, and a clean
environment. Only the declared executable is materialized; package supporting
files are not extracted. Scripts need a shebang such as `#!/bin/sh`; binaries
must match the host. The [Docker image](../../Dockerfile) includes `/app/multica`
on the execution PATH and Alpine's shell, but no Python, Node, or jq.

Return JSON on stdout (or no output). Output is capped at 1 MiB; stderr is
discarded so script diagnostics cannot leak credentials into invocation logs.
The existing `timeout_ms` applies: 10 seconds by default, at most 30 seconds.
The process group is killed on timeout and after completion, and the temporary
directory is removed. Launch ordinary agent runs for long work; these hooks
perform short policy checks. Scheduled and event retries retain existing
semantics, so scripts must make repeated deliveries safe.

This hook fragment requires `"scopes": ["host:exec"]` in its manifest:

```json
{
  "key": "check",
  "name": "Check issues",
  "description": "Apply this package's workflow policy.",
  "triggers": ["schedule"],
  "schedule": { "cron": "*/5 * * * *", "timezone": "UTC" },
  "transport": { "type": "exec", "entry": "hooks/check.sh" }
}
```

## Remaining core work

**Supply credentials automatically.** On owner/admin installation, create
a dedicated [PAT](../../server/internal/handler/personal_access_token.go) for
that account and keep its value encrypted with the installation using existing
[plugin secret storage](../../server/pkg/db/queries/plugin.sql). Inject
`MULTICA_TOKEN`, `MULTICA_WORKSPACE_ID` and `MULTICA_SERVER_URL` into the
ordinary account CLI environment. Disable/uninstall revokes it; re-enabling
creates a replacement. Keep credentials out of ZIPs, user-entered config,
browser surfaces and logs.

This is trusted admin-installed code. PAT authority is account-wide; manifest
scopes and the workspace variable do not restrict it or sandbox the script.
Make that authority clear in the installation preview and retain ordinary API
permission checks. No package-specific permission API is needed.

Reuse the [scheduler](../../server/internal/scheduler/jobs_plugin_hook.go).
Scripts periodically read current state and avoid repeating recorded work.
Existing events can accelerate checks, but the
[plugin dispatcher](../../server/internal/service/plugin_event_dispatch.go)
can drop events under load. Scheduled rechecks remain the baseline; the richer
native wakeup event catalog is not automatically a plugin hook catalog.

## Existing V1 manifest examples

`multica.plugin.json` declares a package's contributions; it is not executable
workflow logic. The ZIP also contains any referenced frontend scripts or skills.
HTTP/MCP hooks execute through their declared external endpoints; `exec` runs
the declared artifact on the Multica host.

This is a complete example of the existing scheduled HTTP hook format.
The endpoint is illustrative; an actual installation needs its handler deployed.

```json
{
  "manifest_version": 1,
  "key": "com.example.research",
  "name": "Research workflow",
  "version": "1.0.0",
  "author": { "name": "Workspace admin" },
  "scopes": ["issues:read", "net:workflow.example.com"],
  "config": {
    "price_limit_usd": {
      "type": "number",
      "label": "Issue spending limit (USD)",
      "required": true
    }
  },
  "contributes": {
    "hooks": [{
      "key": "check",
      "name": "Check issues",
      "description": "Check issue state and reported spending.",
      "triggers": ["schedule"],
      "schedule": { "cron": "*/5 * * * *", "timezone": "UTC" },
      "transport": {
        "type": "http",
        "url": "https://workflow.example.com/check"
      }
    }]
  }
}
```

Other supported fragments below fit into that manifest. Hook variants retain
the required `key`, `name` and `description`; remove `schedule` when replacing
the schedule trigger.

| Contribution | Existing JSON fragment |
| --- | --- |
| Event-triggered hook | `{"triggers":["event"],"events":["issue.created","issue.status_changed"]}` |
| Manual/UI HTTP hook | `{"triggers":["manual","ui"]}` |
| Agent MCP hook | `{"triggers":["agent"],"transport":{"type":"mcp","url":"https://workflow.example.com/mcp"}}` |
| Entry in `contributes.surfaces` | `{"key":"panel","type":"issue_panel","name":"Research","entry":"ui/main.js"}` |
| Modal surface | `{"key":"details","type":"modal","name":"Details","entry":"ui/details.js"}` |
| Entry in `contributes.resources` | `{"type":"skill","key":"research","entry":"skills/research/SKILL.md"}` |

Existing scheduled hooks support cron intervals of at least five minutes.
The package chooses its cadence through `schedule.cron`. Available event names
are `issue.created`, `issue.updated`, `issue.status_changed`, `comment.created`,
`task.started`, `task.completed` and `task.failed`; existing event scopes apply.

Shipped examples: [Schedule Pulse](../../examples/plugins/schedule-pulse/multica.plugin.json),
[Triage Notify](../../examples/plugins/triage-notify/multica.plugin.json),
[Hello Panel](../../examples/plugins/hello-panel/multica.plugin.json), and
[Deploy Sentinel](../../examples/plugins/deploy-sentinel/multica.plugin.json).

## First packages

| Package | Additional policy only |
| --- | --- |
| Parent wakeups | Keep native child-done behavior. Add configured blocked/review notifications to the parent using supported wakeup conditions or scheduled inspection; avoid duplicate rules or runs. |
| Ralph loop | Check opted-in unfinished issues, active runs, children and wakeups on the package's schedule. Request another ordinary run when appropriate, respecting human handoffs, stop conditions and package limits. |
| Issue spending threshold | Read `multica issue usage ISSUE --output json`; compare reported cost with configured `price_limit_usd`, then comment or cancel runs. |

Use configured project/issue targets and their workflow statuses. Spending uses
existing `cost_usd_ticks` and coverage fields: unreported usage means this is a
periodic threshold check, not an instantaneous cap.

## Delivery

1. **Host support:** implement the two additions above. Verify entry validation,
   timeout/termination, failure reporting, retries, credential handling and
   disable/re-enable. Preserve existing HTTP/MCP behavior.
2. **Packages:** prove the runner with the spending check, then parent wakeups
   and Ralph. Verify repeated checks, native handoffs, custom statuses, human
   steps and pending runs. Keep these policies in package scripts.
3. **Production acceptance:** install and upgrade ZIPs in running Docker without
   entering a PAT or rebuilding/restarting Multica. Verify that publishing alone
   does not upgrade an installation, and separately test scheduler recovery after
   restart. Agent-authored versions use the same publish/upgrade path.

Track [task/agent reads #7620](https://github.com/multica-ai/multica/pull/7620)
and [issue listing/task creation #8851](https://github.com/multica-ai/multica/pull/8851),
both open at review. Existing CLI/API access lets this work proceed independently.
Maintainers [declined a separate Goal Mode](https://github.com/multica-ai/multica/issues/7185#issuecomment-5338335094),
supporting a small optional Ralph policy. Hosted execution and automatic PATs
are our self-hosted extension, not an upstream commitment; the
[July 2 maintainer position](https://github.com/multica-ai/multica/issues/4490#issuecomment-4862955743)
reserved plugin-core development for the internal team.
