---
name: multica-parallel-refinement
description: Refine a bound solution with a supplied test script, measured results and foreground experiment recalls.
---

Run `python3 .pi/skills/multica-parallel-refinement/wrapper.py` in the solution
workspace. Follow the returned recalls in the same turn until completion. Each
parallel issue owns its own workspace and task record.
Use `--help` to replay the saved decision without running checks or actions.
Before starting or after completion, it shows these instructions without starting
a task. The HTML state graph offers the same `--help` view.

Supply the Python test script at configuration. It is bound to this workspace:
`validate() -> bool` checks the delivery format, `score(delivery) -> number` ranks
the full JSON result, and `is_complete(delivery) -> bool` defines completion.
Running the script tests the current solution and writes that JSON to stdout;
write progress to stderr. Store the script outside the solution folder.

Describe each template or idea in one paragraph. The script tests a missing
baseline before variant work begins. Review each variant result in one paragraph.
During test, inspect the returned log excerpt and choose `continue` or `exception`
immediately; the full logs remain at the printed paths. Test completion or
exception advances to review. See [DEBUG.md](DEBUG.md) for the states.

In every state, follow returned error guidance and finish unmet prerequisites
before retrying. Reconcile writes first. For assertion failures, use the returned
`repaired` recall once; report a repeated failure through native Multica block.
