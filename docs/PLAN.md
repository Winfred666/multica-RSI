# Tree Layout Plan

Implemented behavior is documented in the [fork development reference](engineering/fork-development.md#frontend).

## Planned: compact sibling subtrees

Measure each sibling subtree as a block, including its descendants, nested workspaces and artifacts. Pack those blocks into the available space with a small gap, preserving readable connections and preventing overlap. Recalculate after card or artifact content changes.

## Planned: drag and remember root trees

Status: proposed; implement only when requested.

Let a person drag a root tree as one unit while its descendants and embedded workspaces keep their relative positions. Use canvas coordinates after undoing pan/zoom, and keep node navigation, rectangular selection, Space-to-pan, and content measurement working. Choose an explicit root drag handle to avoid competing with these existing interactions.

The current automatic layout is temporary. A future draggable layout would store a custom layout model, which might require backend changes. Decide that persistence contract before implementing dragging. Unpositioned roots use automatic packing. Preserve saved positions through filtering; remove obsolete entries only when deletion is confirmed. A reset action should clear the current view's manual positions and restore automatic layout.

Before shipping, verify dragging at non-default zoom, nested workspace containment, reload persistence, workspace/view isolation, reset, and the behavior when a saved root becomes a child. Define that last transition explicitly rather than silently moving unrelated trees. Keep the same interaction in web and desktop.

## SOP calls and cross-issue handoffs

Local symbolic SOP invocations belong to their agent task. A remote caller uses
`multica remote-sop ISSUE SOP [args...]`, which posts the non-waking note
`/note !sop <name> <JSON argv>`. The daemon takes it through its heartbeat,
moves it into the execution log and runs the skill's `wrapper.py` with task ID
`0`. Remote calls never enter agent admission, steering or provider execution.
Skills come from agent bindings and workspace `.agents/` folders. No registration
or issue metadata write is needed. Ordinary cross-issue work continues to use
native steering or concrete mentions.

`multica-subissue-distribute` is the parent's child creation and dispatch SOP.
Labels select query, development or experiment behavior. Children post results,
measured scores, commits, reasons and evidence in native reply comments and enter
`in_review` while awaiting acceptance. Use `multica-surface-block` for an external
blocker. Delivery follows the [textual issue workflow](../harness/skills/multica-issue-workflow/SKILL.md);
`done` stays human.

Keep inter-SOP communication in replies and steering prompts. Issue metadata holds
native resource bindings and configuration, without copied reports, thread IDs,
delivery IDs or review dispatch records. SOP task JSON is local execution state.
