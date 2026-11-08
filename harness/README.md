# Workspace startup kit

`manifest.json` selects the system skills, optional library skills and plugins.
`startup.gotmpl` is the workspace's editable startup prompt. It includes the issue
workflow, common CLI contracts and a final Never-HERO/Stop-slop block. Routine issue turns
need no `multica-issue-turn` read; the kit no longer includes that skill.
Startup carries common checkout rules. Read `multica-project-resource-contract`
only for supplied benchmarks or fixed resource/submission contracts; its evaluator
and canonical submission rules remain available to every agent as a system skill.
Current issue bodies travel in the per-turn claim prompt, keeping the startup prefix
stable. Older server/daemon pairs still supply the explicit issue-read command.

Workspace creation leaves the kit empty. Onboarding imports the checklist
selection through the native workspace package API; an empty selection installs
nothing.

The kit keeps one owner per deliverable, reuses current-task context and worktrees,
establishes working artifacts early and preserves measured improvements. Delegation
remains available for independently useful work. Lifecycle SOPs handle child review,
delivery and blockers. `multica-subissue-distribute` creates children from the parent;
children use `multica-surface-delivery` or `multica-surface-block`. Replies carry
results and native steering asks the parent to review or help. Each SOP runs
within one agent task and issue.

The optional `harness-backup-restore` client fetches the server's embedded default
package through the native API. It exports and imports the same package format
as the workspace UI and CLI; the server owns installation and validation. Backup
reads count/summary pages before downloading selected content once. Restore
rejects existing names unless `--replace-existing` authorizes replacement; install
updates the named startup kit. See the [package contract](../docs/engineering/workspace-packages.md).
System skills inherit into newly created agents; optional skills remain in the
workspace library for attachment.

Plugin packages and checks live in [plugins/](plugins/README.md). Startup publishes
and installs their bundles without provisioning execution credentials. Configure
hosted execution separately with an owner/admin PAT before running hooks.
[Grilling](skills/grilling/SKILL.md) is from
Matt Pocock's MIT-licensed skills collection; its license is included alongside it.

```sh
cd harness
go test ./...
```
