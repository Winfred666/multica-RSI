# Workspace startup kit

`manifest.json` selects the system skills, optional library skills and plugins.
`startup.gotmpl` is the workspace's editable startup prompt. It routes issue turns
to `multica-issue-workflow` only when that skill is available, and keeps common CLI
contracts and the final Never-HERO/Stop-slop block. Acceptance and benchmark
delivery rules live in the skill's textual reference, so masking the skill removes
those procedures without removing the writing rules.
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
remains available for independently useful work. `multica-subissue-distribute`
creates children from the parent;
`multica-surface-block` handles external blockers. Children answer naturally and
enter `in_review` while awaiting acceptance. The textual
acceptance policy lets parents accept children and scopes benchmark completion
rules to benchmark work; other root completion stays human.
The textual system skill follows the issue workflow in official Multica upstream
[`73975653`](https://github.com/multica-ai/multica/blob/73975653323750d04c27047ea9c986ef06bdf6a1/server/internal/daemon/execenv/runtime_config_sections.go#L614-L653).
That revision defines no separate delivery or sub-issue review skill.

The optional `harness-backup-restore` client fetches the server's embedded default
package through the native API. It exports and imports the same package format
as the workspace UI and CLI; the server owns installation and validation. Backup
reads count/summary pages before downloading selected content once. Restore
replaces matching names/keys within the destination workspace; install updates
the named startup kit. See the [package contract](../docs/engineering/workspace-packages.md).
System skills inherit into newly created agents; optional skills remain in the
workspace library for attachment.
The optional library contains `harness-backup-restore` and [explicit workspace manipulation](skills/multica-workspace-manipulation/SKILL.md).
Workspace manipulation requires an authorized host member profile or descriptor; ordinary
task credentials and remote-sop do not grant cross-workspace access. It is not a
system skill and is never attached automatically. Mika onboarding guidance renders in the chat system
prompt and applies only when product-authored onboarding context is present.

Plugin packages and checks live in [plugins/](plugins/README.md). Startup publishes
and installs their bundles without provisioning execution credentials. Configure
hosted execution separately with an owner/admin PAT before running hooks.

```sh
cd harness
go test ./...
```

## Symbolic SoPs

The CLI owns the Python engine, Workflow API and authoring kit. Use
`multica sop dev init DIRECTORY --minimal` (or `--feature-rich`) to start a skill,
`multica sop docs` for its guide, and `multica sop dev validate DIRECTORY`
for validation. Authoring guidance is separate from ordinary procedure help.

The two executable system SoPs declare `metadata: {sop: symbolic}` in `SKILL.md`
and keep only their procedure instructions, controller and domain helpers.
Run `multica sop run multica-surface-block` or
`multica sop run multica-subissue-distribute`; `--help` replays saved guidance
without executing actions. The engine preserves the caller's working directory
and stores current status and recovery in the issue's `sop.<name>` metadata key.
Preserve those records and controller semantics when updating installed skills.

Procedure-specific checks use the CLI-owned modules from the source tree:

```sh
PYTHONPATH=server/internal/cli/sop python3 -m unittest discover -s harness/tests
multica sop dev validate harness/skills/multica-surface-block
multica sop dev validate harness/skills/multica-subissue-distribute
```

An optional `config.json` with `{"DEBUG": true}` enables the engine's compiled state HTML.
The graph derives from Workflow declarations; fenced DOT action docstrings add
local logic. Preview publication failures leave procedure results intact.
