"use client";

import { useCallback, useEffect, useMemo, useRef, useState, type PointerEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { EyeOff, Plus, RotateCcw } from "lucide-react";
import type { AgentTask, Issue, Project } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { isBuiltInIssueStatus } from "@multica/core/issue-statuses";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { issueStatusCategory, statusColumnKeys } from "@multica/core/issues";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { isEditableShortcutTarget } from "@multica/core/shortcuts";
import {
  buildIssueTree, issueTreeCollaborators, layoutIssueTree, TREE_NODE_WIDTH,
} from "@multica/core/issues/surface/tree";
import { Button } from "@multica/ui/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useZoomCanvas } from "../../editor/hooks/use-zoom-canvas";
import { ZoomCanvas, ZoomControls } from "../../editor/zoom-canvas";
import { useT } from "../../i18n";
import { sortIssues } from "../utils/sort";
import { useStatusLabel } from "../utils/status-label";
import { useIssueTimelineQuery } from "../hooks/use-issue-timeline";
import type { IssueCreateDefaults } from "../surface/types";
import type { IssueStatusPagination } from "../surface/use-issue-status-branches";
import { useIssueSurfaceSelection } from "../surface/selection-context";
import { IssueHoverCard } from "./issue-hover-card";
import { BoardCardContent } from "./board-card";
import type { ChildProgress } from "./list-row";
import { IssueActionsContextMenu } from "../actions";
import { TreeCollaboratorAvatar, TreeIssueHoverContent } from "./tree-hover-content";
import { BUILT_IN_ICON_COLOR, StatusIcon } from "./status-icon";

const EMPTY_TASKS: AgentTask[] = [];

export function TreeView({ issues, statusPagination, onCreateIssue, childProgressMap, projectMap }: {
  issues: Issue[];
  statusPagination: IssueStatusPagination;
  onCreateIssue: (defaults?: IssueCreateDefaults) => void;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const statusLabel = useStatusLabel(wsId);
  const hiddenStatuses = useViewStore((s) => s.hiddenStatuses);
  const statusFilters = useViewStore((s) => s.statusFilters);
  const hideStatus = useViewStore((s) => s.hideStatus);
  const restoreStatus = useViewStore((s) => s.showStatus);
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const treeViewport = useViewStore((s) => s.treeViewport);
  const setTreeViewport = useViewStore((s) => s.setTreeViewport);
  const { data: tasks = EMPTY_TASKS } = useQuery(agentTaskSnapshotOptions(wsId));
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [heights, setHeights] = useState<ReadonlyMap<string, number>>(new Map());
  const onHeightChange = useCallback((id: string, height: number) => {
    if (height > 0) setHeights((previous) => previous.get(id) === height ? previous : new Map(previous).set(id, height));
  }, []);
  const layout = useMemo(() => layoutIssueTree(buildIssueTree(
    sortIssues(issues, sortBy, sortDirection, statusColumnKeys(catalog)),
    new Set(hiddenStatuses.filter((status) => !statusFilters.includes(status))),
    expanded,
  ), heights), [issues, sortBy, sortDirection, catalog, hiddenStatuses, statusFilters, expanded, heights]);
  const canvas = useZoomCanvas({ content: layout, initialTransform: treeViewport, preserveTransform: true, panWithSpace: true });
  const selection = useIssueSurfaceSelection();
  const canvasRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{ pointerId: number; x: number; y: number; selected: Set<string> } | null>(null);
  const suppressClick = useRef(false);
  const [rectangle, setRectangle] = useState<{ x: number; y: number; width: number; height: number } | null>(null);

  function contentPoint(event: PointerEvent<HTMLElement>) {
    const bounds = canvasRef.current!.getBoundingClientRect();
    return {
      x: (event.clientX - bounds.left - canvas.transform.x) / canvas.transform.scale,
      y: (event.clientY - bounds.top - canvas.transform.y) / canvas.transform.scale,
    };
  }

  function handlePointerDown(event: PointerEvent<HTMLElement>) {
    suppressClick.current = canvas.isSpacePressed;
    if (canvas.isSpacePressed || event.pointerType === "touch") {
      canvas.handlePointerDown(event);
      return;
    }
    if (event.button !== 0 || (event.target as Element).closest("[data-tree-card], button, a, input, textarea, select")) return;
    event.preventDefault();
    event.currentTarget.focus({ preventScroll: true });
    event.currentTarget.setPointerCapture?.(event.pointerId);
    const point = contentPoint(event);
    drag.current = { pointerId: event.pointerId, ...point, selected: new Set(event.shiftKey || event.ctrlKey || event.metaKey ? selection.selectedIds : []) };
    setRectangle({ ...point, width: 0, height: 0 });
  }

  function handlePointerMove(event: PointerEvent<HTMLElement>) {
    const start = drag.current;
    if (!start || start.pointerId !== event.pointerId) {
      canvas.handlePointerMove(event);
      return;
    }
    const point = contentPoint(event);
    const rect = { x: Math.min(start.x, point.x), y: Math.min(start.y, point.y), width: Math.abs(point.x - start.x), height: Math.abs(point.y - start.y) };
    suppressClick.current = true;
    setRectangle(rect);
    const selected = new Set(start.selected);
    for (const { node, x, y, height } of layout.nodes) {
      if (!node.collapsed && x + TREE_NODE_WIDTH / 2 >= rect.x && x - TREE_NODE_WIDTH / 2 <= rect.x + rect.width && y + height >= rect.y && y <= rect.y + rect.height) selected.add(node.issues[0]!.id);
    }
    selection.deselect([...selection.selectedIds].filter((id) => !selected.has(id)));
    selection.select([...selected]);
  }

  function handlePointerUp(event: PointerEvent<HTMLElement>) {
    if (drag.current?.pointerId === event.pointerId) {
      drag.current = null;
      setRectangle(null);
    }
    canvas.handlePointerUp(event);
  }
  useEffect(() => {
    setTreeViewport(canvas.transform);
  }, [canvas.transform, setTreeViewport]);
  const byId = useMemo(() => new Map(layout.nodes.map((node) => [node.node.id, node])), [layout]);
  const pages = Object.values(statusPagination);
  const pending = pages.some((page) => page.hasMore || page.isFetching);
  const failed = pages.some((page) => page.isError);
  useEffect(() => {
    // A canvas needs all matching branches to place parents and children correctly.
    for (const page of Object.values(statusPagination)) {
      if (page.hasMore && !page.isFetching && !page.isError) page.loadMore();
    }
  }, [statusPagination]);

  function color(issue: Issue) {
    if (isBuiltInIssueStatus(issue.status)) return { className: BUILT_IN_ICON_COLOR[issue.status], color: undefined };
    const custom = catalog.colorOf(issue.status);
    const fallback = STATUS_CONFIG[issueStatusCategory(issue) ?? "unstarted"].iconColor;
    return { color: custom ?? undefined, className: custom ? undefined : fallback };
  }

  return (
    <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-muted/15" style={{ backgroundImage: "radial-gradient(var(--border) 1px, transparent 1px)", backgroundSize: "24px 24px" }}
      onClickCapture={(event) => {
        if (suppressClick.current && canvasRef.current?.contains(event.target as Node)) {
          suppressClick.current = false;
          event.preventDefault();
          event.stopPropagation();
          return;
        }
        const card = (event.target as Element).closest<HTMLElement>("[data-tree-card]");
        if (card && selection.selectedIds.size > 0 && (event.target as Element).closest("a")) {
          event.preventDefault();
          event.stopPropagation();
          selection.toggle(card.dataset.treeCard!);
        } else if (!card && canvasRef.current?.contains(event.target as Node)) {
          selection.clear();
        }
      }}
    >
      <div className="flex shrink-0 items-center justify-between gap-3 px-4 py-2">
        <span className="text-caption text-muted-foreground" role="status">
          {failed ? t(($) => $.tree.load_failed) : pending ? t(($) => $.tree.loading) : t(($) => $.tree.pan_hint)}
        </span>
        <div className="flex items-center gap-1">
          {failed && <Button variant="ghost" size="sm" onClick={() => pages.filter((page) => page.isError).forEach((page) => page.retry())}>{t(($) => $.status_catalog_error.retry)}</Button>}
          {expanded.size > 0 && <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.tree.collapse)} onClick={() => setExpanded(new Set())}><RotateCcw /></Button>}
          <Popover>
            <PopoverTrigger render={<Button variant="ghost" size="sm" />}><EyeOff />{t(($) => $.tree.hide_statuses)}</PopoverTrigger>
            <PopoverContent align="end" className="w-56 space-y-2">
              {statusColumnKeys(catalog).map((status) => (
                <label key={status} className="flex items-center justify-between gap-3 text-caption">
                  <span className="flex items-center gap-2"><StatusIcon status={status} color={catalog.colorOf(status)} icon={catalog.iconOf(status)} />{statusLabel(status)}</span>
                  <Switch size="sm" checked={hiddenStatuses.includes(status)} onCheckedChange={(checked) => checked ? hideStatus(status) : restoreStatus(status)} />
                </label>
              ))}
            </PopoverContent>
          </Popover>
        </div>
      </div>
      <ZoomCanvas canvas={{ ...canvas, handlePointerDown, handlePointerMove, handlePointerUp,
        handleKeyDown: (event) => {
          if (!isEditableShortcutTarget(event.target)) {
            if (event.key === "Escape") selection.clear();
            if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "a") {
              event.preventDefault();
              selection.select(layout.nodes.filter(({ node }) => !node.collapsed).map(({ node }) => node.issues[0]!.id));
              return;
            }
          }
          canvas.handleKeyDown(event);
        },
      }} canvasRef={canvasRef} className={cn("tree-canvas", canvas.isSpacePressed && "is-space-pressed")} content={layout} label={t(($) => $.view.tree)}>
        {rectangle && <div aria-hidden="true" data-tree-selection className="pointer-events-none absolute z-10 border border-brand bg-brand/10" style={{ left: rectangle.x, top: rectangle.y, width: rectangle.width, height: rectangle.height }} />}
        <svg width={layout.width} height={layout.height} className="pointer-events-none absolute inset-0" aria-hidden="true">
          {layout.nodes.map((position) => {
            const parent = position.parentId ? byId.get(position.parentId) : null;
            if (!parent) return null;
            const startY = parent.y + parent.height;
            const endY = position.y;
            const midY = (startY + endY) / 2;
            const dx = position.x - parent.x;
            const radius = Math.min(12, Math.abs(dx) / 2);
            const direction = Math.sign(dx);
            const path = dx === 0
              ? `M ${parent.x} ${startY} V ${endY}`
              : `M ${parent.x} ${startY} V ${midY - radius} Q ${parent.x} ${midY} ${parent.x + direction * radius} ${midY} H ${position.x - direction * radius} Q ${position.x} ${midY} ${position.x} ${midY + radius} V ${endY}`;
            const tone = color(position.node.issues[0]!);
            return <path key={position.node.id} d={path} fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" className={tone.className} style={{ color: tone.color, opacity: 0.65 }} />;
          })}
        </svg>
        {layout.nodes.map(({ node, x, y }) => (
          <div key={node.id} className="absolute flex flex-col items-center" style={{ left: x - TREE_NODE_WIDTH / 2, top: y, width: TREE_NODE_WIDTH }}>
            <TreeIssueNode
              issue={node.issues[0]!}
              tasks={tasks}
              onCreateIssue={onCreateIssue}
              childProgress={childProgressMap?.get(node.issues[0]!.id)}
              project={projectMap?.get(node.issues[0]!.project_id ?? "")}
              onHeightChange={onHeightChange}
              collapsed={node.collapsed ? {
                id: node.id,
                count: node.issues.length,
                onExpand: () => setExpanded((previous) => new Set([...previous, node.id])),
              } : undefined}
            />
          </div>
        ))}
      </ZoomCanvas>
      <ZoomControls canvas={canvas} className="absolute bottom-4 right-4 rounded-lg border bg-background p-1 shadow-sm" />
    </div>
  );
}

function TreeIssueNode({ issue, tasks, onCreateIssue, childProgress, project, onHeightChange, collapsed }: {
  issue: Issue;
  tasks: AgentTask[];
  onCreateIssue: (defaults?: IssueCreateDefaults) => void;
  childProgress?: ChildProgress;
  project?: Project;
  onHeightChange: (id: string, height: number) => void;
  collapsed?: { id: string; count: number; onExpand: () => void };
}) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const selection = useIssueSurfaceSelection();
  const selected = selection.selectedIds.has(issue.id);
  const ref = useRef<HTMLDivElement>(null);
  const nodeId = collapsed?.id ?? issue.id;
  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    const measure = () => onHeightChange(nodeId, element.offsetHeight);
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [nodeId, onHeightChange]);
  // shortcut: one cached timeline per displayed issue; batch collaborators if large trees need it.
  const timeline = useIssueTimelineQuery(issue.id);
  const collaborators = useMemo(() => issueTreeCollaborators(timeline.data ?? []), [timeline.data]);
  const report = collaborators.filter((actor) => actor.actorType === "agent").flatMap((actor) => actor.messages)
    .sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))[0];

  const card = (
    <BoardCardContent
      issue={issue}
      editable={!collapsed}
      childProgress={childProgress}
      project={project}
      titleContent={collapsed ? issue.title :
        <IssueHoverCard issueId={issue.id} content={<TreeIssueHoverContent issue={issue} report={report} timeline={timeline} />}>
          <span tabIndex={0}>{issue.title}</span>
        </IssueHoverCard>
      }
      collaborators={collaborators.length > 0 ? (
        <span className="flex flex-wrap gap-1.5" aria-label={t(($) => $.display.card_collaborators)}>
          {collaborators.map((collaborator) => (
            <TreeCollaboratorAvatar key={`${collaborator.actorType}:${collaborator.actorId}`} collaborator={collaborator} issue={issue} tasks={tasks} />
          ))}
        </span>
      ) : null}
    />
  );

  if (collapsed) return (
    <div ref={ref} className="group/card relative isolate w-full pb-4" data-tree-card={nodeId}>
      <div aria-hidden="true" className="absolute inset-x-4 top-4 bottom-0 rounded-lg border border-surface-border bg-muted" />
      <div aria-hidden="true" className="absolute inset-x-2 top-2 bottom-2 rounded-lg border border-surface-border bg-surface" />
      <div inert className="relative">{card}</div>
      <Button variant="ghost" className="absolute inset-0 h-auto w-full rounded-lg hover:bg-transparent" aria-label={t(($) => $.tree.expand, { count: collapsed.count })} aria-description={issue.title} onClick={collapsed.onExpand}>
        <span className="absolute -right-2 -top-2 rounded-full border bg-background px-1.5 text-micro tabular-nums">{collapsed.count}</span>
      </Button>
    </div>
  );

  return (
    <div ref={ref} className="w-full" data-tree-card={issue.id}>
      <IssueActionsContextMenu issue={issue}>
        <div className={cn("group/card rounded-lg", selected && "outline outline-1 outline-brand")} data-selected={selected ? "" : undefined}>
          <AppLink href={paths.issueDetail(issue.id)} newTabTitle={issue.identifier} className="block">
            {card}
          </AppLink>
        </div>
      </IssueActionsContextMenu>
      <div className="flex justify-center">
        <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.tree.create_child)} onClick={() => onCreateIssue({ parent_issue_id: issue.id, parent_issue_identifier: issue.identifier, project_id: issue.project_id })}><Plus /></Button>
      </div>
      {timeline.isError && <Button variant="ghost" size="xs" onClick={() => void timeline.refetch()}>{t(($) => $.tree.retry_agents)}</Button>}
    </div>
  );
}
