# Hosted workflow plugins

Status: hosted execution and an explicitly supplied PAT per workspace implemented.

## Decision

Reuse native workflows, wakeups and the plugin scheduler. Core supplies packaged
execution, workspace credentials and idempotent wakeup creation. Workflow
policy ships as independently upgraded ZIPs inside the deployed Multica
container. One initial host release is required; subsequent package changes
reuse the workspace credential and need no further Multica deployment.

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
on the execution PATH, Alpine's shell, and Python 3. Node and jq are not included.

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

## Workspace credentials

Hosted execution is disabled in each workspace until an owner/admin explicitly
supplies a PAT in **Settings → Plugins → Hosted execution**. The PAT determines
the execution account, displayed as **Run as**. Its owner must be a member of
that workspace; they can be an ordinary member. Supply a token its owner has
authorized for this purpose. Multica never creates a PAT automatically.

All hosted plugins in that workspace share the encrypted credential.
Installation, upgrades, and per-plugin enable/disable do not change it or its
owner. Each workspace has independent configuration; the host never falls back
to another workspace's token. A dedicated PAT per workspace allows independent
revocation, although the same account can own several PATs.

The host requires `MULTICA_PLUGIN_SECRET_KEY` (a base64-encoded 32-byte key)
and `MULTICA_PUBLIC_URL` (the API origin reachable from the host). Each invocation
receives `MULTICA_TOKEN`, `MULTICA_WORKSPACE_ID`, and `MULTICA_SERVER_URL`.
Credentials are write-only: reads return status and owner metadata. Token values
are absent from ZIPs, JSON input, configuration responses and logs; accidental
echoes in hook stdout are redacted.

Execution checks the PAT's validity and its owner's workspace membership before
launching. Expiry, revocation, or departure blocks subsequent launches. Disabling
workspace execution or deleting the workspace removes the stored copy; it does
not revoke the supplied PAT, which may have other uses. Already running hooks
can finish within their timeout. The owner can revoke the PAT separately.

Hosted scripts are trusted, unsandboxed code. A standard PAT is account-wide:
workspace configuration and manifest scopes do not restrict its authority.
CLI writes belong to the PAT owner. The existing invocation callback remains
available for actions needing plugin attribution and scoped grants.

## API and CLI

These endpoints use ordinary account authentication and workspace membership.
Only owners/admins may change the credential; members can read its status.

| Operation | Endpoint |
| --- | --- |
| Read status and execution owner | `GET /api/workspaces/{id}/plugins/execution` |
| Supply or replace the PAT and enable execution | `PUT /api/workspaces/{id}/plugins/execution` with `{"token":"mul_…"}` |
| Disable execution and forget the stored copy | `DELETE /api/workspaces/{id}/plugins/execution` |

GET/PUT return `configured`, `enabled`, and `run_as` (`{id, name}` or `null`),
never the token. `configured: true, enabled: false` means the saved credential
or host prerequisites are no longer usable.

```sh
multica workspace plugin-execution status acme
multica workspace plugin-execution enable acme --token-stdin < /secure/plugin-pat
multica workspace plugin-execution disable acme
multica workspace plugin list acme
multica workspace plugin configure INSTALLATION_ID acme --config-file prompts.json
```

The workspace argument also accepts a UUID/prefix, or can be omitted to use the
CLI's selected workspace. Commands return JSON. The supplied token is separate
from the admin credential used to authenticate the configuration request.

## Workflow policy

Workflow policy belongs in plugin packages. Sub-issue hooks react to issue
events and create deduplicated native wakeups; Ralph reacts to completed runs.
Neither package uses recurring polling. The
[plugin dispatcher](../../server/internal/service/plugin_event_dispatch.go)
can drop events under load; manual sub-issue checks can re-evaluate current
conditions. Other packages can use the existing
[scheduler](../../server/internal/scheduler/jobs_plugin_hook.go) when needed.

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
| [Wakeup for sub-issue](../../plugins/wakeup-for-sub-issue/multica.plugin.json) | Three configurable prompts handle all children finished, child review, and child blocked. Issue events create one native wakeup per transition. Saved transition IDs preserve deduplication when older timeline activity is truncated. |
| [In-progress Ralph](../../plugins/in-progress-ralph/multica.plugin.json) | On task completion, continue the affected idle in-progress issue, respecting active/deferred runs, unfinished children, pending wakeups and human handoffs. |
| [Issue reframe compress](../../plugins/issue-reframe-compress/multica.plugin.json) | Review and clean issue comments within the assignee's current run after two reported context compressions. |
| Issue spending threshold | Read `multica issue usage ISSUE --output json`; compare reported cost with configured `price_limit_usd`, then comment or cancel runs. |

The packages are available in [plugins/](../../plugins/README.md).
The retired built-in child-done settings return an empty list; writes return 410.
Migration 596 disables their existing rules and cancels their queued/deferred
runs. Explicit user-created wakeup conditions remain available.

Spending uses
existing `cost_usd_ticks` and coverage fields: unreported usage means this is a
periodic threshold check, not an instantaneous cap.

## Delivery

1. **Host support (implemented):** packaged execution and workspace credentials
   reuse the existing dispatcher, scheduler and account API. No remaining core
   work from this proposal is required to start authoring these packages.
2. **Packages (implemented):** parent wakeups and Ralph ship as independent ZIPs.
   Their scripts check repeated deliveries, custom statuses, human steps and
   pending runs. Spending checks remain a future package.
3. **Production acceptance:** install and upgrade ZIPs in running Docker without
   re-entering the workspace PAT or rebuilding/restarting Multica. Verify that publishing alone
   does not upgrade an installation, and separately test scheduler recovery after
   restart. Agent-authored versions use the same publish/upgrade path.

Hosted execution and workspace credentials are extensions maintained by this fork.
