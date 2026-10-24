# Hosted workflow plugins

These packages use the existing event hooks, scheduler, plugin storage and issue
APIs. Policy changes ship as versioned ZIPs. Python 3 is the only runtime
dependency and is included in the server image.

| Package | Behavior | Settings |
| --- | --- | --- |
| Wakeup for sub-issue | Wake the parent agent or squad leader when all direct children are done/cancelled, or a child enters review/blocked. | `all_finished_prompt`, `in_review_prompt`, `blocked_prompt` |
| In-progress Ralph | On task completion, immediately continue the affected issue if it is still idle and in progress. | `prompt` |
| Issue reframe compress | At a natural stopping point, clean discussion within the assignee’s existing run after two reported context compressions. | None |

## Build and install

```sh
python3 plugins/package.py plugins/wakeup-for-sub-issue /tmp/wakeup-for-sub-issue.zip
python3 plugins/package.py plugins/in-progress-ralph /tmp/in-progress-ralph.zip
python3 plugins/package.py plugins/issue-reframe-compress /tmp/issue-reframe-compress.zip
```

First enable **Settings → Plugins → Hosted execution** with an explicitly supplied
PAT. The displayed account must be able to invoke the intended agents. Upload
each ZIP in Plugins, install it, set any required prompts, and enable it. Blank prompts
do no work. There are no workspace IDs, URLs or credentials in the packages.
See [host setup and API](../docs/engineering/hosted-workflow-plugins.md).

Fork upgrades move the plugin migrations from `585–588` to `593–596` to avoid
upstream migration numbers. Their existing guards allow them to run again on
databases that applied the former filenames, preserving workspace execution
credentials. No migration history edits are needed.

The settings are also available through the CLI:

```sh
multica workspace plugin list WORKSPACE
multica workspace plugin configure INSTALLATION_ID WORKSPACE --config-file prompts.json
```

`prompts.json` is a JSON object of setting names and prompt strings, for example:

```json
{"prompt":"Read the current issue and latest result. Continue the remaining work. If the deliverable is ready, move it to review; if external input is required, mark it blocked and explain what is needed."}
```

This calls `PUT /api/workspaces/{id}/plugins/{installationId}/config` with
`{"values":{...}}`. Sub-issue prompts use the three names in the table above.
Publishing a changed package requires a new manifest version and an explicit
installation upgrade; configuration edits need neither a ZIP nor a host restart.

## Delivery and deduplication

Sub-issue hooks react to issue events and recheck every five minutes. They defer
while a parent has an active/deferred run; they skip closed, backlog, human-assigned
and unassigned parents. Current status categories support custom board statuses.
Ralph listens only to `task.completed`, checks the affected issue, and posts the
configured prompt as a comment mentioning its current agent or squad leader.
The comment triggers continuation immediately, without a timer or cooldown.
It skips active/deferred runs, unfinished children, enabled wakeups and issues
whose latest run is not the completed event's task. Failed/cancelled events do
not trigger Ralph. Plugin storage deduplicates processed task IDs.
Enabling a package applies it throughout that workspace.

Issue reframe compress reads `input_trend.context_compression_count` from the
current agent assignee's issue task runs. This is the existing platform count
(a completed-run input trend), not a new measurement of provider compaction.
The agent calls `Issue comments` at a natural stopping point in its normal work.
There are no timer/event hooks, cleanup wakeups, or separate cleanup runs. The
plugin checks the existing count and starts cleanup after two increments. Its
baseline is stored per issue and assignee; the platform's usage history stays
intact. The baseline resets after all eligible comments have been reviewed.
The final tool result returns the current run's latest triggering/delivered
comment, or its handoff instruction. Without an outer instruction it says
"Continue solving the issue." Steering is read again at completion, so a message
delivered during cleanup takes precedence. The agent continues its original work
without posting a cleanup comment. Tool use is opportunistic, not forced by a
runtime hook; the new description is available on subsequent normal runs.

The `Issue comments` tool omits resolved threads and automatically deletes eligible
system-authored comments. It returns up to 20 unresolved comments for the assignee
to choose `delete`, `update` with one short result/conclusion, or `preserve` for
important experiments, guidance, and knowledge. Updates clear old attachments;
preserved comments remain unchanged. The script protects the latest reply of each
thread, active-run inputs, pending supplements, and comments newer than the cleanup
cutoff. It rechecks eligibility before each write and uses comment revisions for
updates. It does not modify the issue title/body or stored execution logs.

Install with a workspace owner/admin PAT that can edit/delete the relevant
comments. No additional model or service is required. If the comment API reports
a truncated window, cleanup stops without advancing its baseline. The normal
comment-delete endpoint has no conditional revision parameter, so its final
read/delete pair is not atomic; the plugin does not change that core API.

Scripts store processed transitions in installation-scoped workspace storage.
Sub-issue wakeup creation uses `idempotency_key` (at most 256 bytes): retries under
the same issue and account return the existing rule, even after that one-shot was
consumed. This covers concurrent deliveries and a crash between queueing and
saving state for sub-issue wakeups.
Repeated review/block/completion cycles rearm when a check observes the condition
become false. A brief transition missed by both events and scheduled reads is
not guaranteed delivery. Ralph events are best-effort; there is no polling
recovery. Its comment and deduplication write are separate requests, so concurrent
delivery or a failed storage write can produce duplicate comments.

Hooks must finish within 30 seconds and use the existing plugin storage quota
(1,000 keys per scope). Larger workspaces need batched scans and state pruning.
Disable the installation to stop future checks; an already-created ordinary
wakeup or running agent task is managed separately on its issue. The built-in
automatic child-done rule is retired; explicit user-created waits still work.

## Check

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s plugins -v
```
