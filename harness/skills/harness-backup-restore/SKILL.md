---
name: harness-backup-restore
description: Export, import or install a Multica workspace's startup prompt, skills, agents, MCPs and plugins.
---

Run `python3 <this-skill-directory>/scripts/harness.py --help` with the target
workspace's `MULTICA_SERVER_URL`, `MULTICA_WORKSPACE_ID` and `MULTICA_TOKEN`.
This optional client uses the native workspace package API. Workspace creation
starts empty; onboarding imports the selected startup resources.

```sh
python3 <this-skill-directory>/scripts/harness.py backup --output harness-backup.json
python3 <this-skill-directory>/scripts/harness.py restore --input harness-backup.json
python3 <this-skill-directory>/scripts/harness.py install
```

An owner/admin member credential is required. Packages contain private instructions
and MCP configuration; keep them local. Runtime credentials, user grants, plugin
secrets and execution PATs are excluded.

Install fetches the server's default kit, updates its named skills and plugins and
sets the startup prompt. `--keep-context` preserves the prompt;
`--without-plugins` excludes plugins from install or backup. Installation does not
create a PAT. Hosted plugin execution requires separate credential setup.

Backup reads bounded inventory summaries, then downloads selected harness content
once in the same package format used by the workspace UI and CLI. Restore replaces
matching skill/MCP names and plugin keys within the destination workspace. System-skill classification
cannot change on import; published plugin code stays immutable and differing code
requires a new version. Agents receive new identities.
Imported agents are offline and private, owned by the importer, and need a runtime
binding before use. Projects/issues are excluded from harness backups. Import
changes commit together or roll back together.
