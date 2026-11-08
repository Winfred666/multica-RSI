---
name: multica-subissue-distribute
description: Create and dispatch query, dev or exp children from the parent issue.
---

Use in the parent issue. Run `python3 <this-skill-directory>/wrapper.py` and follow
its choices. Supply a child specification JSON with `title` and optionally
`description_file`, `project`, `stage`, `priority`, `due_date` and `assignee_id`.
`--start` requires an assignee. `query` shares the parent branch; `dev` and `exp`
receive their own prepared branches and inherited sandbox.

The first `exp` requires `--baseline-score SCORE --direction min|max`.
Competitive siblings share `--experiment-group GROUP --experiment-group-size N`;
create all members before waiting. Ordinary children report independently.

Each child follows its own `multica-surface-delivery` or `multica-surface-block`.
Native steering brings its reply back; use `multica-subissue-review` for acceptance.
Follow returned error guidance, reconcile writes before retrying, and use `repaired`
once for assertion failures; report a repeated failure through `multica-surface-block`.

See [DEBUG.md](DEBUG.md) for this procedure’s states. In every state, follow
returned error guidance and finish unmet prerequisites before retrying. Reconcile
writes first. For assertion failures, use the returned `repaired` callback once;
report a repeated failure through native Multica block.
