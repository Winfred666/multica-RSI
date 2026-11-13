# Acceptance and delivery

## Final answers

Choose the final answer's format by issue label.

### `dev`

Use the `query` format for question replies. Omit progress updates unless necessary; keep them minimal.

Keep code minimal and scoped. Open with one sentence naming the feature or fix and its key method
and linking its commit on the issue's branch. Give the full diff against the
integration base: `+A / -D lines across N files`. List at most the three
most-changed files; totals include all files. Do not append details about other files outside the table.

| Relative path | Added / deleted | Change |
| --- | --- | --- |
| `path/to/file` | +A / -D | One sentence explaining the change. |

Give the decisive behavioral check result in one clause, not pass counts or a checklist. Report confirmed defects only if they threaten
correctness or block acceptance. For reverts or cancellations, amend the original
delivery comment in place, not by appending another report; replace stale claims,
commit links and counts.

### `exp`

Keep code minimal and reproducible. Open with one sentence giving the authorized
benchmark and metric, baseline → result, improvement or regression, and any fallback.
Explain the key method in one short paragraph with evidence and a reproduction
command or source link. Use a publication-ready plot or grid if useful.
End with one sentence: "Student's next step or choices, pending approval: …"

### `query`

Open with a clear verdict supported by one short paragraph of quantitative or
theoretical evidence. Link authoritative primary sources or mainstream community
references beside the evidence. Use a compact table or visual if useful.

## Child acceptance

A child submits verified work as `in_review` and waits for parent acceptance;
it does not set itself `done`. The reviewing parent may accept a child. Review
a query's answer against its request without requiring a commit or merge.
For explicit dev/exp, review the committed deliverable, merge it, verify
integration and clean up its branch before setting the child `done`. No pull
request is required.

Preserve rejected work, return the same child to `in_progress`, and request the
correction in a comment mentioning its actual assignee. That mention dispatches
the correction; a status change or unmentioned comment alone is insufficient.
Parent acceptance replaces the human-only completion default for children.

Keep acceptance limited to the stated deliverable. Do not require unsolicited
hashes/checksums, speculative cases, scoring rubrics or infrastructure; do not
block, reopen or reject otherwise correct work for their absence. Add those
requirements only when the user explicitly requests them.

## Benchmark delivery

Apply this section only to supplied benchmarks or bounded research attempts.
Follow the project's selected improvement, resource and sub-issue procedures.

Benchmark roots are explicitly dev: their project startup kit supplies the root
branch and persistent worktree before assignment. Query and untagged issues do
not bootstrap branches; an attached repository alone does not change the SOP tag.
The selected sub-issue SOP resolves `git_repo_root` from the current issue or
its ancestors using `MULTICA_ISSUE_ID`; repository arguments are resolved by
script. Root metadata `git_branch` and `git_workdir` identify its branch and
worktree. Native task directories are bootstrap context. Repository selection
and inherited execution belong to the SOP.

Commit the first valid canonical artifact with reproducible source and evidence
before further tuning; retain the best valid checkpoint. Use root metadata
`aide_trial_deadline_at` to reserve time for integration, verification and
delivery. Finish required descendant work, verify the canonical root commit,
report the measured result and set the root `done`, including an honest target
miss. Done ends the attempt; the unchanged grader decides target attainment.
Do not create verdict-only continuation turns.
