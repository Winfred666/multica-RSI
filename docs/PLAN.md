# Tree Layout Plan

## Current layout

Each issue or sub-workspace occupies its measured size. Children start below their own parent, so a tall artifact or workspace does not stretch unrelated branches. The layout measures each root tree as a bounding box and packs those boxes into rows, preserving root order. The target row width derives from total tree area and the visible canvas aspect ratio. This is a simple packing heuristic, not an optimal bin packer.

The implementation lives in `packages/core/issues/surface/tree.ts`, workspace composition in `packages/core/workspace/tree.ts`, and canvas measurement in `packages/views/issues/components/tree-view.tsx`. Web and desktop share it. Pan and zoom remain local view preferences.

## Planned: drag and remember root trees

Status: proposed; implement only when requested.

Let a person drag a root tree as one unit while its descendants and embedded workspaces keep their relative positions. Use canvas coordinates after undoing pan/zoom, and keep node navigation, rectangular selection, Space-to-pan, and card resizing working. Choose an explicit root drag handle to avoid competing with these existing interactions.

Store only root offsets in the shared core view store through its existing persistence adapter. Scope them to workspace and view, keyed by stable root issue ID. Do not write layout to issue metadata, persist server payloads, or add backend endpoints. Unpositioned roots use automatic packing. Preserve saved positions through filtering; remove obsolete entries only when deletion is confirmed. A reset action should clear the current view's manual positions and restore automatic layout.

Before shipping, verify dragging at non-default zoom, nested workspace containment, reload persistence, workspace/view isolation, reset, and the behavior when a saved root becomes a child. Define that last transition explicitly rather than silently moving unrelated trees. Keep the same interaction in web and desktop.
