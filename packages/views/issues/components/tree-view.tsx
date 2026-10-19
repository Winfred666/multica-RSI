"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { EyeOff, FileText, MoreHorizontal, Plus, RotateCcw, UserRound } from "lucide-react";
import type { AgentTask, Issue } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { isBuiltInIssueStatus } from "@multica/core/issue-statuses";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { issueTimelineOptions } from "@multica/core/issues/queries";
import { issueStatusCategory, statusColumnKeys } from "@multica/core/issues";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import {
  buildIssueTree, issueTreeParticipants, layoutIssueTree,
  TREE_NODE_HEIGHT, TREE_NODE_WIDTH,
} from "@multica/core/issues/surface/tree";
import { Button } from "@multica/ui/components/ui/button";
import { BorderBeam } from "@multica/ui/components/common/border-beam";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@multica/ui/components/ui/hover-card";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { RichContent } from "../../rich-content";
import { useZoomCanvas } from "../../editor/hooks/use-zoom-canvas";
import { ZoomCanvas, ZoomControls } from "../../editor/zoom-canvas";
import { useT } from "../../i18n";
import { sortIssues } from "../utils/sort";
import { useStatusLabel } from "../utils/status-label";
import type { IssueCreateDefaults } from "../surface/types";
import type { IssueStatusPagination } from "../surface/use-issue-status-branches";
import { IssueMentionCard } from "./issue-mention-card";
import { BUILT_IN_ICON_COLOR, StatusIcon } from "./status-icon";

const EMPTY_TASKS: AgentTask[] = [];

export function TreeView({ issues, statusPagination, onCreateIssue }: {
  issues: Issue[];
  statusPagination: IssueStatusPagination;
  onCreateIssue: (defaults?: IssueCreateDefaults) => void;
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
  const { data: tasks = EMPTY_TASKS } = useQuery(agentTaskSnapshotOptions(wsId));
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const layout = useMemo(() => layoutIssueTree(buildIssueTree(
    sortIssues(issues, sortBy, sortDirection, statusColumnKeys(catalog)),
    new Set(hiddenStatuses.filter((status) => !statusFilters.includes(status))),
    expanded,
  )), [issues, sortBy, sortDirection, catalog, hiddenStatuses, statusFilters, expanded]);
  const canvas = useZoomCanvas({ content: layout });
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
    <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-muted/15" style={{ backgroundImage: "radial-gradient(var(--border) 1px, transparent 1px)", backgroundSize: "24px 24px" }}>
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
      <ZoomCanvas canvas={canvas} content={layout} label={t(($) => $.view.tree)}>
        <svg width={layout.width} height={layout.height} className="pointer-events-none absolute inset-0" aria-hidden="true">
          {layout.nodes.map((position) => {
            const parent = position.parentId ? byId.get(position.parentId) : null;
            if (!parent) return null;
            const startY = parent.y + (parent.node.collapsed ? 88 : TREE_NODE_HEIGHT);
            const endY = position.y + (position.node.collapsed ? 40 : 12);
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
          <div key={node.id} className="absolute flex flex-col items-center" style={{ left: x - TREE_NODE_WIDTH / 2, top: y, width: TREE_NODE_WIDTH, height: TREE_NODE_HEIGHT }}>
            {node.collapsed ? (
              <Button variant="outline" size="icon-lg" className="relative mt-10 rounded-full bg-background shadow-[4px_4px_0_var(--muted),8px_8px_0_var(--border)]" aria-label={t(($) => $.tree.expand, { count: node.issues.length })} onClick={() => setExpanded((previous) => new Set([...previous, node.id]))}>
                <MoreHorizontal />
                <span className="absolute -right-2 -top-2 rounded-full border bg-background px-1 text-micro tabular-nums">{node.issues.length}</span>
              </Button>
            ) : <TreeIssueNode issue={node.issues[0]!} tasks={tasks} onCreateIssue={onCreateIssue} />}
          </div>
        ))}
      </ZoomCanvas>
      <ZoomControls canvas={canvas} className="absolute bottom-4 right-4 rounded-lg border bg-background p-1 shadow-sm" />
    </div>
  );
}

function TreeIssueNode({ issue, tasks, onCreateIssue }: {
  issue: Issue;
  tasks: AgentTask[];
  onCreateIssue: (defaults?: IssueCreateDefaults) => void;
}) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  // shortcut: one cached timeline per displayed issue; batch participants if large trees need it.
  const timeline = useQuery(issueTimelineOptions(issue.id));
  const participants = useMemo(() => issueTreeParticipants(issue, timeline.data ?? [], tasks), [issue, timeline.data, tasks]);
  const hasReportStage = issue.status === "blocked" || issue.status === "in_review" || issueStatusCategory(issue) === "done";
  const agents = participants.agentIds;
  const visibleAgents = agents.slice(0, 6);
  if (participants.primaryId && !visibleAgents.includes(participants.primaryId)) visibleAgents.splice(5, 1, participants.primaryId);
  const extraAssignee = issue.assignee_type && issue.assignee_type !== "agent" && issue.assignee_id;

  return (
    <>
      <div className="flex w-full items-center justify-center gap-1 rounded-md bg-background/95 py-1">
        <div className="min-w-0"><IssueMentionCard issueId={issue.id} issue={issue} fallbackLabel={issue.identifier} /></div>
        {hasReportStage && (
          <HoverCard>
            <HoverCardTrigger render={<Button variant="ghost" size="icon-xs" aria-label={t(($) => $.tree.report)} />}><FileText /></HoverCardTrigger>
            <HoverCardContent className="w-96 max-w-[90vw]">
              <p className="mb-2 text-caption font-medium">{t(($) => $.tree.report)}</p>
              {timeline.isError ? <Button variant="outline" size="sm" onClick={() => void timeline.refetch()}>{t(($) => $.status_catalog_error.retry)}</Button> : participants.report?.content ? (
                <div className="max-h-80 overflow-auto"><RichContent content={participants.report.content} /></div>
              ) : <p className="text-caption text-muted-foreground">{timeline.isPending ? t(($) => $.tree.loading) : t(($) => $.tree.no_report)}</p>}
            </HoverCardContent>
          </HoverCard>
        )}
      </div>
      <div className="h-4 w-px shrink-0 bg-border" />
      <div className="flex max-w-full items-center justify-center gap-2 rounded-full bg-background px-2 py-1">
        <div className="flex -space-x-2">
          {extraAssignee && <ActorAvatar actorType={issue.assignee_type!} actorId={extraAssignee} size="xl" enableHoverCard />}
          {visibleAgents.map((id, index) => (
            <span key={id} className={cn("relative inline-flex rounded-full border-2 border-background", participants.runningIds.has(id) && "bg-brand/10")} style={{ zIndex: agents.length - index }} title={`${getActorName("agent", id)}${id === participants.primaryId ? ` · ${t(($) => $.table.columns.assignee)}` : ""}`}>
              <ActorAvatar actorType="agent" actorId={id} size="xl" enableHoverCard hoverCardVariant="live" />
              {participants.runningIds.has(id) && <BorderBeam />}
            </span>
          ))}
          {!extraAssignee && agents.length === 0 && <span className="inline-flex size-10 items-center justify-center rounded-full border bg-muted text-muted-foreground" title={t(($) => $.swimlane.no_assignee)}><UserRound className="size-5" /></span>}
        </div>
        {agents.length > 6 && <Popover><PopoverTrigger render={<Button variant="ghost" size="icon-xs" aria-label={t(($) => $.tree.helpers)} />}>+{agents.length - 6}</PopoverTrigger><PopoverContent className="max-h-64 w-56 overflow-auto space-y-2">{agents.map((id) => <div key={id} className="flex items-center gap-2 text-caption"><ActorAvatar actorType="agent" actorId={id} size="md" />{getActorName("agent", id)}</div>)}</PopoverContent></Popover>}
        <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.tree.create_child)} onClick={() => onCreateIssue({ parent_issue_id: issue.id, parent_issue_identifier: issue.identifier, project_id: issue.project_id })}><Plus /></Button>
      </div>
      {timeline.isError && <Button variant="ghost" size="xs" onClick={() => void timeline.refetch()}>{t(($) => $.tree.retry_agents)}</Button>}
    </>
  );
}
