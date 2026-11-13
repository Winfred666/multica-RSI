---
name: multica-greeting
description: Demonstrate natural greeting handover or direct questionnaire publication.
metadata: {sop: symbolic}
---

Expose this asset's parent as `MULTICA_SOP_SKILL_ROOTS` in a disposable issue.
Run `multica sop run multica-greeting`. Choose `greet` for a natural reply, or
`publish` when strict output requires a direct SoP comment. Supply the intended
human/agent's name with `--recipient`. Direct publication requires the existing
SoP-authored comment API and task-token authentication.

Follow `instruction`: a natural reply ends the turn and hands over to its named
recipient. With silence guidance, end without an acknowledgement. The recipient
selects/fills the declared questionnaire, then the sender resumes normally.
`--help` shows ordinary saved choices without publishing again.
Follow error guidance; reconcile writes before retrying. Use `repaired` once for
an assertion failure and report repeated failure through native Multica block.
