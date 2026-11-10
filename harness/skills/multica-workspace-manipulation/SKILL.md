---
name: multica-workspace-manipulation
description: Explicitly create, inspect, import, export and dispatch work in another workspace using a host-authorized member context.
---

Use for deliberate cross-workspace work, not ordinary task CLI access. This is an
optional library skill, not a system skill. Run
`python3 <this-skill-directory>/wrapper.py` in the originating issue context.
Follow returned choices in the same turn; `--help` replays guidance without acting.
Keep `MULTICA_ISSUE_ID`, `MULTICA_WORKSPACE_ID`, `MULTICA_TOKEN` and server URL bound
to the originating task. Only the member subprocess targets the selected workspace.

## Authorization and execution

Skill attachment is not authorization. An authorized host operator must launch
each privileged callback with `MULTICA_WORKSPACE_MEMBER_FD` naming an inherited
file descriptor (3 or higher) containing a member PAT. Use a fresh descriptor per
callback. Provision it from the host's secret facility, never from task metadata,
agent environment configuration, packages, arguments or tracked files. Never read
the daemon profile to manufacture this context. The PAT stays in process memory
and the member CLI child's environment, not SOP state or command output. The
member CLI runs with a temporary empty config and working directory, without task
identity or daemon access. `MULTICA_CLI_PATH` may select the trusted host CLI.
The original task credential is used only for native SOP state.

An operator launcher can use Python `subprocess.run(..., pass_fds=(fd,))` with a
pipe read descriptor in `MULTICA_WORKSPACE_MEMBER_FD`; write the PAT to the pipe
privately and close the write end before launching. Do not echo or log its contents.
This is trusted host execution, not a security sandbox for arbitrary skill code.
Task-only runs fail closed. Remote-sop supplies a task token and does not provision
this descriptor. Unattended invocation requires a separately authorized host/runtime
handoff; do not substitute a PAT into remote-sop's normal task environment.

## Operations

Create an empty workspace or select its UUID. Inspect/import use native package
files and optional selection JSON. Export requires explicit selection and a new
private output path. Keep packages private: they may contain instructions and MCP
configuration. The server's native format, membership checks and import rules apply.
Import never dispatches work. Imported agents are offline and need runtime binding.
No plugin activation is implicit; configure hosted execution separately using a
separate key and `FF_PLUGINS_V1=true` for isolated plugin trials.

The `run --argv-json` choice accepts a JSON argv array beginning with `project`,
`issue`, `agent` or `runtime`, without a shell. Server/workspace/profile overrides
are rejected. For example, supply these arrays through the returned run callback:

```json
["project", "create", "--title", "Trial"]
["runtime", "list"]
["agent", "create", "--name", "Trial worker", "--runtime-id", "RUNTIME_ID"]
["agent", "update", "AGENT_ID", "--runtime-id", "RUNTIME_ID", "--status", "idle"]
["issue", "create", "--title", "Execute trial", "--project", "PROJECT_ID", "--assignee-id", "AGENT_ID", "--status", "todo"]
["issue", "runs", "ISSUE_ID"]
```

Use IDs returned in that workspace. Runtime registration must already authorize
that member/host; use native runtime setup if none is available. Verify a real
run through `issue runs` and native task messages, not workspace creation alone.
Output is bounded; use focused native reads for details. Reconcile external writes
before retrying. Follow error guidance and use a returned `repaired` command once
after correcting the cause. Finish clears only this SOP's originating issue state.
