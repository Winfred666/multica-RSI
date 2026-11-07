# Startup harness investigation

Window: 7 November 2026, 10:04:28–11:04:28 UTC, the hour before R-188.
R-184 through R-187 launched in this window. The three
[fixtures](../../harness/testdata/startup-cases.json) cover agent setup, a delegated
review and external research. Each records the first ten tool calls, source run
and comment IDs, observed delivery size, and a shorter delegation/delivery example.
Runtime skill paths use `<runtime-skills>` in place of host home directories.
Examples are proposed wording, not rerun results or edits to historical issues.

## Startup calls

| Fixture | First ten tool calls | First task-specific operation |
| --- | --- | --- |
| R-184 | Issue read, six skill reads, comment scan, agent/tool inspection, execution | Agent inspection at call 9, 28 seconds after run start |
| R-185 | Issue read, seven skill reads, parent/current comment reads and CLI help | Agent configuration inspection at call 11, 49 seconds after run start |
| R-186 | Issue read, four skill reads, comment scan, service/help checks, source extraction, status/delivery setup, execution | Source extraction at call 8, 36 seconds after run start |

All three opened `multica-issue-turn/SKILL.md` and its 8,882-character workflow
reference. R-185 also opened the 31,472-character issue contract and the
22,698-character agent contract. Some reads supplied task-specific knowledge;
the duplicated basic workflow was avoidable. These counts describe tool calls,
not model reasoning steps, and timestamps do not isolate the cost of each read.

R-187 provided a coding cross-check: its first four calls read the issue, workflow
skill, comments and several references; repository inspection followed. Its later
repeat worktree/status reads reinforce the need to reuse established facts.

## Delegation and delivery

Whitespace counts from the expanded delivery comments: R-184 131 words, R-185
451, R-186 1,720 and R-187 249. Counts exclude attachments. Detailed research
was part of R-186's assignment, so length alone does not establish waste.
The avoidable pattern was repeating qualification in each alternative, then
repeating limitations, process history and unchanged-scope statements.

R-185's title was already useful: “Exercise FOMO Radar on a real implementation
review.” Its 1,562-character body restated workflow procedures, acceptance and
several prohibitions. The fixture keeps the commissioned outcome, required blind
query, source evidence, walkthrough distinction and one-shot scope in a shorter
body. R-186 was deliberately standalone rather than a child: that boundary
protected solution-blind research and must survive compression. Its delivery
needed the recommendation, decisive sources and material uncertainty; a requested
long comparison belongs in an artifact linked from the handoff.

The proposed examples preserve pending acceptance, unknown savings and the
researcher's involvement in the implementation. They do not claim that shorter
wording has improved agent quality or elapsed time.

## Change

The startup template now contains the basic issue lifecycle, bounded comment
catch-up, CLI formatting, resources, child handoff and mention semantics.
`multica-issue-turn` and its references are removed from the kit and workspace. Specialized SOPs still own delivery, delegation and acceptance.

The claim adds an optional `issue_description` field from the already loaded issue
row. Together with the existing title, status, assignee and parent fields, it
supplies the current assignment in the per-turn message for assignment, comment
and wakeup runs. Empty bodies count as supplied context. Unchanged resumed runs
reuse the existing comparison hint instead of repeating the body. The cached startup
prefix stays stable. An older server omits the field and retains its explicit read
instruction; installed older daemons ignore the new field.

The final prompt block distills [HERO](https://github.com/wanshuiyin/HERO-Anti-OverDefense)
and stop-slop: avoid speculative scaffolding, redundant checks and defensive
prose; retain required verification, citations, measurements and material limits.
This changes guidance rather than enforcing word limits or weakening acceptance.

Updating this workspace's template and removing the skill takes effect on subsequent
runs. Skipping the issue CLI read additionally requires the changed backend and
daemon; replacing those running services is separate deployment work.


## Verification

Passed the focused daemon prompt/template checks, two database-backed claim cases
(empty and multiline bodies), the Go kit installer check, 10 harness Python tests,
29 plugin tests, and `e2e/startup-harness.spec.ts`. The Playwright API smoke created
and deleted a fresh workspace on isolated API port 18784, verified the installed
prompt and absence of the retired skill, and created three unassigned backlog
fixtures. No real agent ran. Full frontend/backend suites were not run.

The workspace prompt matches the repository after API whitespace normalization,
and its skill list no longer contains `multica-issue-turn`. The template is 1,977 characters longer than its predecessor and replaces an 8,882-character
mandatory workflow reference plus its skill entry point. This is a prompt-size
comparison, not a measured latency or quality improvement.
