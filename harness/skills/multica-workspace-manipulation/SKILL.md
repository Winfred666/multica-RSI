---
name: multica-workspace-manipulation
description: Create, import, inspect, export, run member CLI commands and wait for inbox events in a chosen workspace.
---

Run `python3 <skill>/wrapper.py --help` (`python` on hosts using that name).

Requires a host-owner-authorized member profile at `~/.multica/config.json`.
Use `--member-config PATH` for an alternate profile. Task credentials do not
authorize these commands. Alternatively, the host can supply
`MULTICA_WORKSPACE_MEMBER_FD` with `MULTICA_SERVER_URL`.
`MULTICA_CLI_PATH` selects the member CLI executable when it is not on PATH.

```sh
python3 <skill>/wrapper.py create --name Trial --slug trial
python3 <skill>/wrapper.py --target WORKSPACE_UUID inspect --file /private/package.json
python3 <skill>/wrapper.py --target WORKSPACE_UUID import --file /private/package.json
python3 <skill>/wrapper.py --target WORKSPACE_UUID export --selection-file /private/selection.json --file /private/export.json
python3 <skill>/wrapper.py --target WORKSPACE_UUID run project create --title Trial
python3 <skill>/wrapper.py --target WORKSPACE_UUID run runtime list
python3 <skill>/wrapper.py --target WORKSPACE_UUID run agent create --name Worker --runtime-id RUNTIME_ID
python3 <skill>/wrapper.py --target WORKSPACE_UUID run issue create --title Trial --project PROJECT_ID --assignee-id AGENT_ID --status todo
python3 <skill>/wrapper.py --target WORKSPACE_UUID run issue runs ISSUE_ID
python3 <skill>/wrapper.py --target WORKSPACE_UUID inbox-wait
```

`create` returns a new workspace ID; the other operations require `--target`.
Use absolute file paths and a new export filename. For imported agents, bind a
runtime with `run agent update AGENT_ID --runtime-id ID`. Creating an agent-assigned
`todo` issue dispatches a task.

`inbox-wait` waits for the oldest unread member notification, returns one compact
JSON event, and marks that issue's notifications read. Output includes the issue,
actor, status, a body preview of up to 500 characters, `body_truncated`,
`details.comment_id` when available, and `next`. Ctrl-C cancels the wait.

Keep member credentials out of command arguments, packages and agent configuration.
