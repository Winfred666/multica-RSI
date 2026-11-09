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
`multica-surface-block` handles external blockers. Children post results as native
issue comments and enter `in_review` while awaiting acceptance. The textual
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
The optional library contains `create-symbolic-sop` for SOP authoring and
`harness-backup-restore`. Mika onboarding guidance renders in the chat system
prompt and applies only when product-authored onboarding context is present.

Plugin packages and checks live in [plugins/](plugins/README.md). Startup publishes
and installs their bundles without provisioning execution credentials. Configure
hosted execution separately with an owner/admin PAT before running hooks.

```sh
cd harness
go test ./...
```

Symbolic SOP authoring and runtime follow AIDE²'s pinned protocol
[`05ccf153`](https://github.com/WinfredClaw/multica-AIDE/tree/05ccf1530a46339af62daa7cf434d777037ff9db/.agents/skills/create-symbolic-sop).
The complete template includes the background experiment monitor, persistent task
facts, scripted routes and separate callback options. Each executable system SOP
ships its own controller diagram, monitor and configuration; `--validate` rejects
missing `DEBUG.md` or a non-boolean `config.json` DEBUG value. RSI keeps its native
single-task lifecycle procedures and documents their actual states. The authoring
reference points to RSI's bundled platform guidance.

Static `DEBUG.md` diagrams are required even when previews are disabled. Set
`config.json` to `{"DEBUG": true}` to publish the controller's state HTML on native
state shifts. Read-only controller `artifacts(context)` can also publish live HTML;
upload failures leave the procedure result intact. Existing `.state` records and
procedure-specific policies must be preserved when updating installed bundles.
