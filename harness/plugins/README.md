# Default workflow plugins

These packages use Multica event hooks, native wakeups, plugin storage and issue
APIs. Python 3 is the only runtime dependency.

| Plugin | Behavior | Settings |
| --- | --- | --- |
| In-progress Ralph | Continue idle in-progress work after completed or failed task runs, respecting active work, child handoffs and explicit waits. | `prompt` |
| Comment and thread pruning | Review stale comments and resolved threads for deletion in batches of five, at compression counts 2, 4, 6 and onward. | None |
| Wakeup for sub-issue | Continue the parent when a child requests review, is blocked or all children finish. | `all_finished_prompt`, `in_review_prompt`, `blocked_prompt` |

These apply across the workspace, including issues without a project. Resolved
threads show only their final comment. Pruning offers delete or keep, protects
active task inputs and changed comments, and leaves issue status unchanged.
The compression wakeup and the end of cleanup include a short continuation:
read the workspace prompt's final two paragraphs with the workspace CLI if reply
rules are forgotten, or reload the assigned issue if its goal is unclear. The agent
runs these commands locally; the hosted plugin does not read runtime files.
Wakeups follow completed tasks at the existing thresholds, not mid-turn compaction.

## Build and install

```sh
python3 harness/plugins/package.py harness/plugins/in-progress-ralph /tmp/in-progress-ralph.zip
python3 harness/plugins/package.py harness/plugins/issue-reframe-compress /tmp/comment-and-thread-pruning.zip
python3 harness/plugins/package.py harness/plugins/wakeup-for-sub-issue /tmp/wakeup-for-sub-issue.zip
```

The workspace startup kit configures hosted execution with the creator's PAT and
installs, configures and enables all three packages. The commands above also
build packages for manual publication. Manual installs use **Settings → Plugins
→ Hosted execution** to supply the PAT.

The default Ralph prompt is:

```json
{"prompt":"Continue the unfinished task from its saved state: keep working or mention an agent for help. When the work is done, run multica-surface-delivery. If the work is truly blocked, run multica-surface-block."}
```

Configure it with:

```sh
multica workspace plugin configure INSTALLATION_ID WORKSPACE --config-file prompts.json
```

Publishing a package does not upgrade its installation. Install the published
version explicitly to apply an update without restarting the host. Disabling a
plugin prevents future hooks; queued wakeups and tasks remain managed on their
issues. See [host setup and API](../../../docs/engineering/hosted-workflow-plugins.md).

## Check

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s harness/plugins -v
```
