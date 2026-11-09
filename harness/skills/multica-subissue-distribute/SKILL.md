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

Each child posts final results as a native issue comment and enters `in_review`
while awaiting acceptance. Use `multica-surface-block` for an external blocker.
Follow returned error guidance, reconcile writes before retrying, and use `repaired`
once for assertion failures; report a repeated failure through `multica-surface-block`.

`python3 <this-skill-directory>/wrapper.py --help` replays the last decision for
this issue, or shows this skill body when no SoP record exists, without running
the procedure or creating state.
