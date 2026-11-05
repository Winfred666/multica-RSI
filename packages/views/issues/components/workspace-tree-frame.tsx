"use client";

import { useMemo } from "react";
import { EyeOff, Layers } from "lucide-react";
import { issueStatusCategory } from "@multica/core/issues";
import { ApiError } from "@multica/core/api";
import { paths } from "@multica/core/paths";
import { buildIssueTree, layoutIssueTree, TREE_NODE_WIDTH } from "@multica/core/issues/surface/tree";
import type { WorkspaceTree } from "@multica/core/workspace/tree";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { StatusIcon } from "./status-icon";

export function WorkspaceTreeFrame({ data, error, retry }: {
  data?: WorkspaceTree;
  error: Error | null;
  retry: () => void;
}) {
  const { t } = useT("issues");
  const layout = useMemo(() => layoutIssueTree(buildIssueTree(data?.issues ?? [], new Set(), new Set())), [data]);
  if (error) return (
    <div data-workspace-frame className="flex w-[300px] items-center justify-center rounded-lg border border-dashed p-3 text-muted-foreground">
      {error instanceof ApiError && [403, 404].includes(error.status)
        ? <EyeOff className="size-4" role="img" aria-label={t(($) => $.tree.workspace_no_access)} />
        : <Button size="xs" variant="ghost" onClick={retry}>{t(($) => $.status_catalog_error.retry)}</Button>}
    </div>
  );
  if (!data) return <div data-workspace-frame role="status" className="w-[300px] rounded-lg border border-dashed p-3 text-caption text-muted-foreground">{t(($) => $.tree.loading)}</div>;
  const href = paths.workspace(data.workspace.slug).issues();
  return (
    <section data-workspace-frame aria-label={data.workspace.name} className="w-[760px] overflow-hidden rounded-lg border border-surface-border bg-background">
      <AppLink href={href} className="flex items-center gap-2 border-b px-3 py-2 text-caption font-medium">
        <Layers className="size-4 shrink-0" /><span className="truncate">{data.workspace.name}</span>
      </AppLink>
      <div className="max-h-[560px] overflow-auto bg-muted/15" tabIndex={0}
        onWheelCapture={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
        <div className="relative" style={{ width: layout.width, height: layout.height }}>
          <svg width={layout.width} height={layout.height} className="pointer-events-none absolute inset-0 text-muted-foreground" aria-hidden="true">
            {layout.nodes.map((position) => {
              const parent = layout.nodes.find((p) => p.node.id === position.parentId);
              return parent ? <path key={position.node.id} d={`M ${parent.x} ${parent.y + parent.height} V ${position.y - 24} H ${position.x} V ${position.y}`} fill="none" stroke="currentColor" /> : null;
            })}
          </svg>
          {layout.nodes.map(({ node, x, y }) => {
            const issue = node.issues[0]!;
            return <AppLink key={issue.id} href={paths.workspace(data.workspace.slug).issueDetail(issue.id)} newTabTitle={issue.identifier}
              className="absolute flex h-24 flex-col gap-2 overflow-hidden rounded-lg border border-surface-border bg-surface p-3 hover:bg-muted"
              style={{ left: x - TREE_NODE_WIDTH / 2, top: y, width: TREE_NODE_WIDTH }}>
              <span className="flex items-center gap-2 text-micro text-muted-foreground"><StatusIcon status={issue.status} category={issueStatusCategory(issue) ?? undefined} /><span>{issue.identifier}</span></span>
              <span className="line-clamp-2 text-caption">{issue.title}</span>
            </AppLink>;
          })}
        </div>
      </div>
    </section>
  );
}
