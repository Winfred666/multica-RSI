# Tree Layout Plan

## Current layout

Each issue or sub-workspace occupies its measured size. Children start below their own parent, so a tall artifact or workspace does not stretch unrelated branches. The layout measures each root tree as a bounding box and packs those boxes into rows, preserving root order. The target row width derives from total tree area and the visible canvas aspect ratio. This is a simple packing heuristic, not an optimal bin packer.

The implementation lives in `packages/core/issues/surface/tree.ts`, workspace composition in `packages/core/workspace/tree.ts`, and canvas measurement in `packages/views/issues/components/tree-view.tsx`. Web and desktop share it. Pan and zoom remain local view preferences.

## Planned: compact sibling subtrees

Measure each sibling subtree as a block, including its descendants, nested workspaces and artifacts. Pack those blocks into the available space with a small gap, preserving readable connections and preventing overlap. Recalculate after card or artifact resizing.

## Planned: drag and remember root trees

Status: proposed; implement only when requested.

Let a person drag a root tree as one unit while its descendants and embedded workspaces keep their relative positions. Use canvas coordinates after undoing pan/zoom, and keep node navigation, rectangular selection, Space-to-pan, and card resizing working. Choose an explicit root drag handle to avoid competing with these existing interactions.

Store only root offsets in the shared core view store through its existing persistence adapter. Scope them to workspace and view, keyed by stable root issue ID. Do not write layout to issue metadata, persist server payloads, or add backend endpoints. Unpositioned roots use automatic packing. Preserve saved positions through filtering; remove obsolete entries only when deletion is confirmed. A reset action should clear the current view's manual positions and restore automatic layout.

Before shipping, verify dragging at non-default zoom, nested workspace containment, reload persistence, workspace/view isolation, reset, and the behavior when a saved root becomes a child. Define that last transition explicitly rather than silently moving unrelated trees. Keep the same interaction in web and desktop.

## SOP calls and cross-issue handoffs

One symbolic SOP invocation belongs to one agent task in one issue. Call another
SOP locally by following its entry command. To request work in another issue or
from another agent, use native steering or a mention with a concrete goal and
ask the receiving model to call the named SOP. Do not execute a shared SOP state
across agents or change issue identity to call it remotely.

`multica-subissue-distribute` is the parent's child creation and dispatch SOP.
Labels select query, development or experiment behavior. Children deliver through
`multica-surface-delivery` or explain an external blocker through
`multica-surface-block`. Results, measured scores, commits, reasons and evidence
belong in native reply comments. The child-wakeup plugin carries the reply and
Git bindings in parent steering: review deliveries through
`multica-subissue-review`, or help resolve a blocker. The parent owns acceptance;
verified code integration precedes marking the child done.

Keep inter-SOP communication in replies and steering prompts. Issue metadata holds
native resource bindings and configuration, without copied reports, thread IDs,
delivery IDs or review dispatch records. SOP task JSON is local execution state.
