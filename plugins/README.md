# Hosted workflow plugins

These packages use the existing hook scheduler, plugin storage and issue wakeup
API. Policy changes ship as versioned ZIPs. Python 3 is the only runtime
dependency and is included in the server image.

| Package | Behavior | Settings |
| --- | --- | --- |
| Wakeup for sub-issue | Wake the parent agent or squad leader when all direct children are done/cancelled, or a child enters review/blocked. | `all_finished_prompt`, `in_review_prompt`, `blocked_prompt` |
| In-progress Ralph | Every five minutes, continue an idle in-progress issue whose last run completed at least five minutes ago. | `prompt` |

## Build and install

```sh
python3 plugins/package.py plugins/wakeup-for-sub-issue /tmp/wakeup-for-sub-issue.zip
python3 plugins/package.py plugins/in-progress-ralph /tmp/in-progress-ralph.zip
```

First enable **Settings → Plugins → Hosted execution** with an explicitly supplied
PAT. The displayed account must be able to invoke the intended agents. Upload
each ZIP in Plugins, install it, set its prompts, and enable it. Blank prompts
do no work. There are no workspace IDs, URLs or credentials in the packages.
See [host setup and API](../docs/engineering/hosted-workflow-plugins.md).

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
Ralph also skips unfinished children, enabled wakeups, failed/cancelled runs and
issues without a completed run. It continues at most once per completed run.
Enabling either package applies it throughout that workspace.

Scripts store processed transitions in installation-scoped workspace storage.
Wakeup creation uses `idempotency_key` (at most 256 bytes): retries under the same
issue and account return the existing rule, even after that one-shot was consumed.
This covers concurrent deliveries and a crash between queueing and saving state.
Repeated review/block/completion cycles rearm when a check observes the condition
become false. A brief transition missed by both events and scheduled reads is
not guaranteed delivery.

Hooks must finish within 30 seconds and use the existing plugin storage quota
(1,000 keys per scope). Larger workspaces need batched scans and state pruning.
Disable the installation to stop future checks; an already-created ordinary
wakeup or running agent task is managed separately on its issue. The built-in
automatic child-done rule is retired; explicit user-created waits still work.

## Check

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s plugins -v
```
