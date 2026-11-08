---
name: multica-issue-workflow
description: Follow the textual workflow for assigned Multica issues, delivery and acceptance status.
---

1. Read the current issue and supplied parent context; accept explicit unchanged
   reports. Treat `source_context` as history.
2. Scan comments with `--roots-only --summary --compact`, then expand relevant
   threads with `--thread <id> --tail 30`. Reuse supplied deltas or no-new reports.
3. Set `in_progress` before requested work unless already set or forbidden. Keep
   `in_review` unless explicitly returned for rework. Consultation changes no status.
   Complete the assignment within Agent Identity boundaries; reuse its checkout
   and verified evidence.
4. Post final results through `multica issue comment add --content-file <path>`.
   Reply under this turn's trigger; assignments without a trigger use a top-level
   comment. Terminal output is not delivery.
5. Delivered work awaiting acceptance enters `in_review`; unfinished work stays
   `in_progress`; `done` stays human. A `[Project workflow]` overrides built-in
   status commands.

Use `multica-surface-block` for external blockers and
`multica-subissue-distribute` for child creation.

Based on official Multica's [textual issue workflow](https://github.com/multica-ai/multica/blob/73975653323750d04c27047ea9c986ef06bdf6a1/server/internal/daemon/execenv/runtime_config_sections.go#L614-L653).
