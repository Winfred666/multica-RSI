---
name: multica-parallel-refinement
description: Refine a bound solution with a supplied test script, measured results and foreground experiment recalls.
---

Run `python3 .pi/skills/multica-parallel-refinement/wrapper.py` from the current
issue folder. Follow the returned recalls in the same turn until completion. Each
parallel issue owns its own workspace and SoP metadata. Another agent can resume
the same SoP on that issue. On another machine, make the solution, test script and job logs available before
continuing in that issue folder.
Use `--help` to replay the saved decision without running checks or actions.
Before starting or after completion, it shows these instructions without starting
a run. Required inputs are native `MULTICA_ISSUE_ID` and
`MULTICA_WORKSPACE_ID`.

Supply the Python test script at configuration. Tests run in the current issue folder:
`validate() -> bool` checks the delivery format, `score(delivery) -> number` ranks
the full JSON result, and `is_complete(delivery) -> bool` defines completion.
Running the script tests the current solution and writes that JSON to stdout;
write progress to stderr. Store the script outside the solution folder.

Describe each template or idea in one paragraph. The script tests a missing
baseline before variant work begins. Review each variant result in one paragraph.
During test, inspect the returned log excerpt and choose `continue` or `exception`
immediately; the full logs remain at the printed paths. Test completion or
exception advances to review.

Optional HTML artifacts and DEBUG previews publish as independent workspace
resources. The runtime retains explicit IDs in issue metadata `sop_artifacts`
(`{skill_name: {filename: UUID}}`, JSON encoded as a string) and links them through
`deliverables` (a string-encoded UUID array). Completion/quit clears only the
transient `sop.<skill-name>` key; publication IDs and links survive. Repeated
publication replaces current content by ID with no history. A filename names a
local publication slot, never a resource lookup. Ordinary comment attachments
are not converted. Unlinking removes metadata only; publishing again restores
the link. Coordinate concurrent metadata writers. On a publication warning,
reconcile the reported ID and metadata before retrying publication, not the
completed workflow action.

In every state, follow returned error guidance and finish unmet prerequisites
before retrying. Reconcile writes first. For assertion failures, use the returned
`repaired` recall once; report a repeated failure through native Multica block.
