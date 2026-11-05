---
name: harness-backup-restore
description: Back up, restore or install a Multica workspace's startup prompt, skills, agent configuration and plugins.
---

Run `python3 <this-skill-directory>/scripts/harness.py --help` with the target
workspace's `MULTICA_SERVER_URL`, `MULTICA_WORKSPACE_ID` and `MULTICA_TOKEN`.
The script uses Python's standard library and supported Multica APIs.

```sh
python3 <this-skill-directory>/scripts/harness.py backup --output harness-backup.json
python3 <this-skill-directory>/scripts/harness.py restore --input harness-backup.json
python3 <this-skill-directory>/scripts/harness.py install
```

The installed skill carries the startup kit, including its own installer.
From the source kit, pass `install --kit /path/to/harness`.

Back up before replacing an existing harness. Stop active agents before restoring
their configuration. Backups retain workspace identities and are restored only to
their original server and workspace. They contain private instructions; keep them
local. The script reads its API credentials from the environment and excludes
them from backups. Configuration returned by the API can contain private values.

Install updates the kit's named skills and plugins and sets the workspace startup
prompt. `--keep-context` preserves an existing prompt. System skills are installed
before agents inherit them. Other library skills remain available for attachment.
Plugins reuse the creator's incoming PAT for hosted execution. A human session
creates a PAT owned by that creator through the native token API. An enabled
execution credential is preserved on reinstall. Credentials stay in the server's
encrypted execution store. Use `--without-plugins` on install or backup when the
server has disabled plugins.

Restore updates existing agents and restores their skill attachments, backed-up
skills, plugins and prompt. It preserves unrelated resources, issues, projects and
task history. A failed request stops the script; inspect the reported operation
before retrying. Do not claim recovery of
secrets the API redacted or omitted.
