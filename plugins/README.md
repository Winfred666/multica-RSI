# Default workflow plugins

These packages use Multica event hooks, native wakeups, plugin storage and issue
APIs. Python 3 is the only runtime dependency.

| Plugin | Behavior | Settings |
| --- | --- | --- |
| In-progress Ralph | Continue idle in-progress work after completed or failed task runs, respecting active work, child handoffs and explicit waits. | `prompt` |
| Comment and thread pruning | Review stale comments and resolved threads for deletion in batches of five, at compression counts 2, 4, 6 and onward. | None |

Both apply across the workspace, including issues without a project. Resolved
threads show only their final comment. Pruning offers delete or keep, protects
active task inputs and changed comments, and leaves issue status unchanged.

## Build and install

```sh
python3 plugins/package.py plugins/in-progress-ralph /tmp/in-progress-ralph.zip
python3 plugins/package.py plugins/issue-reframe-compress /tmp/comment-and-thread-pruning.zip
```

Enable **Settings → Plugins → Hosted execution** using an owner/admin PAT able
to invoke agents and manage comments. Upload and install both packages, configure
Ralph's prompt, then enable them. They use the same package identities as the
portable AIDE baseline.

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
issues. See [host setup and API](../docs/engineering/hosted-workflow-plugins.md).

## Check

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s plugins -v
```
