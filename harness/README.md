# Workspace startup kit

`manifest.json` selects the system skills, optional library skills and plugins.
`startup.gotmpl` is the workspace's editable startup prompt. It includes the issue
workflow, common CLI contracts and a final Never-HERO/Stop-slop block. Routine issue turns
need no `multica-issue-turn` read; the kit no longer includes that skill.
Current issue bodies travel in the per-turn claim prompt, keeping the startup prefix
stable. Older server/daemon pairs still supply the explicit issue-read command.

Workspace creation
installs this kit directly with Python; no agent run performs setup.

The kit keeps one owner per deliverable, reuses current-task context and worktrees,
establishes working artifacts early and preserves measured improvements. Delegation
remains available for independently useful work. Lifecycle SOPs handle child review,
delivery and blockers. `multica-subissue-distribute` creates children from the parent;
children use `multica-surface-delivery` or `multica-surface-block`. Replies carry
results and native steering asks the parent to review or help. Each SOP runs
within one agent task and issue.

Run the installer with the target workspace's native credentials:

```sh
python3 harness/skills/harness-backup-restore/scripts/harness.py install --kit harness
```

The installed `harness-backup-restore` skill carries this kit and can reinstall it
without a repository checkout. System skills inherit into newly created agents;
optional skills remain in the workspace library for attachment.

Plugin packages and checks live in [plugins/](plugins/README.md). Startup enables
hosted execution with the creator's request PAT. Browser sessions create a
creator-owned plugin PAT through the native token API. Mika's `multica workspace
create` uses its runtime's PAT through a local handoff; the PAT stays outside the
agent environment. [Grilling](skills/grilling/SKILL.md) is from
Matt Pocock's MIT-licensed skills collection; its license is included alongside it.

```sh
cd harness
go test ./...
```
