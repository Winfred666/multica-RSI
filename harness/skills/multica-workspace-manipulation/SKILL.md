---
name: multica-workspace-manipulation
description: Create, import, inspect, export, run member CLI commands and wait for inbox events in a chosen workspace.
---

Run `python3 <skill>/wrapper.py --help` (`python` on hosts using that name).
This library skill is a small CLI appendix, not a SoP. The same Python-standard-
library scripts work independently of the agent provider: `wrapper.py` executes
native member CLI commands; `auth.py` loads the authorized host member profile.
No Bash, Unix sockets, provider paths or inherited-subprocess FD support is needed.

The host owner must authorize use of its existing `~/.multica/config.json` member
profile. Home resolution follows the host OS. Use `--member-config PATH` for a
named/alternate profile (`~/.multica/profiles/NAME/config.json`). The script reads
that source automatically for each action, without saving another PAT copy.
An explicitly inherited `MULTICA_WORKSPACE_MEMBER_FD` is also supported; its server
comes from `MULTICA_SERVER_URL`. `MULTICA_CLI_PATH` selects the trusted host CLI,
otherwise PATH is used. A remote host needs its own authorized member profile or
credential handoff; attaching this skill does not grant host credential access.

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

`inbox-wait` polls the authorized member's target-workspace inbox every five
seconds until an unread notification exists, reads the oldest, prints one JSON
event and exits. It uses the default read API, which also marks unread siblings
on the same issue read. No notifications, replies or tasks are created. Output
contains native issue ID/title, event type, actor type/ID, full body, details
(such as comment ID), current issue status, read state and a one-sentence `next`
prompt: steer briefly if needed, then wait again unless target work is complete
or acceptable. Ctrl-C stops waiting. Run one listener per member/workspace.

Use native absolute paths (including Windows paths) and a new export filename.
Native package selections, membership checks and opt-in plugin activation apply.
Imported agents need runtime binding: `run agent update AGENT_ID --runtime-id ID`.
Confirm an actual run, not workspace creation alone. Reconcile writes before retrying.
`run` accepts workspace resource commands without a shell; local/auth commands and
server/workspace/profile overrides are rejected. The member child has temporary
HOME/USERPROFILE and cwd, with no task/agent/daemon variables. Original task auth
and ordinary CLI checks stay unchanged. Keep PATs out of prompts, argv, exports,
agent configuration and logs. No backend or daemon change is required.
