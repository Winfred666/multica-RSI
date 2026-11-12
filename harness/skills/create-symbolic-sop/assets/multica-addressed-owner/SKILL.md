---
name: multica-addressed-owner
description: Exercise addressed-owner render handoff through the CLI engine.
metadata: {sop: symbolic}
---

Set `MULTICA_SOP_SKILL_ROOTS` to this directory's parent and `SOP_RENDER_OWNER`
to a dictionary containing destination `issue_id`, `thread`, agent `id`, and the
native comment `content`: `/note` or an existing `[@Agent](mention://agent/<id>)`
link. Use real destination IDs; no custom identity or delivery fields are needed. Run
`multica sop dev validate <this-directory>`, then
`multica sop run multica-addressed-owner` and choose `ask` in the native task context.
The final action supplies `this_round_reply_guidance`; only the subsequent state
renderer emits the comment with the existing ordinary, redacted state JSON.
The CLI reply is masked separately. Stage 3 owns forwarding and rendering.
