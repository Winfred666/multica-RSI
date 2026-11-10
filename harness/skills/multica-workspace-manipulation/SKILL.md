---
name: multica-workspace-manipulation
description: Create, import, inspect, export and run member CLI commands in a chosen workspace.
---

Use `python3 <this-skill-directory>/wrapper.py --help`. This optional library skill
uses the native member CLI and package format. It has no SoP state or controller.

For automatic auth, an operator can authorize an existing member CLI profile once:
`python3 <skill>/auth.py /authorized/member-config.json <wrapper arguments>`.
The host launcher reads that selected profile for each action and privately hands
the PAT to the wrapper on a fresh descriptor. Reuse the command or a shell alias
for every action below. No PAT copy is saved, and ordinary CLI auth is unchanged.
The profile source must be explicitly authorized; the skill does not discover one.

The host must explicitly authorize a member PAT through the inherited descriptor
named by `MULTICA_WORKSPACE_MEMBER_FD` (3 or higher). The script retrieves the PAT
from that descriptor only. It never discovers the daemon profile or upgrades a
task token. A host file descriptor can be reused throughout an authorized session;
a pipe needs a fresh descriptor per invocation. Skill attachment and remote-sop's
task credential alone do not grant this authority. Keep the PAT out of prompts,
argv, packages, agent configs and logs. The child has an empty HOME and working
directory, without task/agent/daemon environment variables. Normal task-token
checks remain unchanged. Set `MULTICA_SERVER_URL`; `MULTICA_CLI_PATH` may select a
trusted host CLI. The originating task environment is unchanged.

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
```

Use absolute file paths. Export selections and import behavior are native CLI
rules; packages may contain private instructions and MCP configuration. Use a new
export path to preserve existing files. Imported agents need a destination runtime
binding (`run agent update AGENT_ID --runtime-id RUNTIME_ID`). Runtime setup must
already authorize the member/host. Confirm an actual run, not workspace creation
alone. `run` accepts workspace resource commands without a shell; local/auth
commands and server/workspace/profile overrides are rejected. Reconcile writes
before retrying failures. Plugin activation remains native and opt-in.
