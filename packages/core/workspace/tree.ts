import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { Attachment, Issue, Workspace } from "../types";
import { independentDeliverables, linkedDeliverables } from "../attachments/deliverables";
import { workspaceListOptions } from "./queries";
import { buildIssueTree, layoutIssueTree, type IssueTreeNode, type IssueTreeSize } from "../issues/surface/tree";

export function issueWorkspaceId(issue: Pick<Issue, "metadata">): string | null {
  const id = issue.metadata?.sub_workspace_id;
  return typeof id === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id)
    ? id : null;
}

export interface WorkspaceTree {
  workspace?: Pick<Workspace, "id" | "slug" | "name">;
  issues: Issue[];
  capturedAt: number;
  error?: Error;
}

export function workspaceTreeOptions(wsId: string, roots: Issue[]) {
  return queryOptions({
    queryKey: ["workspace-tree-snapshot", wsId] as const,
    staleTime: "static",
    gcTime: Infinity,
    retry: false,
    retryOnMount: false,
    enabled: roots.some(issueWorkspaceId),
    queryFn: async ({ client }): Promise<Record<string, WorkspaceTree>> => {
      const workspaces = await client.ensureQueryData(workspaceListOptions());
      const snapshots: Record<string, WorkspaceTree> = {};
      const seen = new Set([wsId]);
      const queue = roots.map(issueWorkspaceId);
      for (const id of queue) {
        if (!id || seen.has(id)) continue;
        seen.add(id);
        const workspace = workspaces.find((w) => w.id === id);
        const snapshot = snapshots[id] = { workspace, issues: [] as Issue[], capturedAt: Date.now() };
        if (!workspace) continue;
        try {
          while (true) {
            const page = await api.listIssues({ workspace_id: id, offset: snapshot.issues.length, limit: 100 }, workspace.slug);
            snapshot.issues.push(...page.issues);
            if (snapshot.issues.length >= page.total) break;
            if (!page.issues.length) throw new Error("Workspace issue pagination made no progress");
          }
          queue.push(...snapshot.issues.map(issueWorkspaceId));
        } catch (error) {
          snapshot.issues = [];
          snapshots[id].error = error instanceof Error ? error : new Error(String(error));
        }
        snapshot.capturedAt = Date.now();
      }
      return snapshots;
    },
  });
}

export function layoutWorkspaceTree(roots: IssueTreeNode[], wsId: string, snapshots?: Record<string, WorkspaceTree>, sizes: ReadonlyMap<string, IssueTreeSize> = new Map(), canvasWidth = 1200, deliverables: readonly Attachment[] = []) {
  const seen = new Set([wsId]);
  const measured = new Map(sizes);
  const nested = new Map<string, ReturnType<typeof layoutIssueTree>>();
  const links: { source: string; target: string; kind?: "deliverable" }[] = [];
  function build(nodes: IssueTreeNode[]): ReturnType<typeof layoutIssueTree> {
    const visit = (node: IssueTreeNode): IssueTreeNode => {
      if (node.deliverable) return node;
      const children = node.children.map(visit);
      const id = issueWorkspaceId(node.issues[0]!);
      if (id && id !== wsId && !node.collapsed && (!snapshots || id in snapshots)) {
        const frameId = `workspace:${id}`;
        if (seen.has(id)) links.push({ source: node.id, target: frameId });
        else {
          seen.add(id);
          const snapshot = snapshots?.[id];
          const content = snapshot?.workspace && !snapshot.error
            ? build(buildIssueTree(snapshot.issues, new Set(), new Set())) : undefined;
          if (content) nested.set(frameId, content);
          measured.set(frameId, content ? { width: content.width, height: content.height + 40 } : { width: 300, height: 40 });
          children.push({ id: frameId, issues: node.issues, collapsed: false, children: [] });
        }
      }
      return { ...node, children };
    };
    const layout = layoutIssueTree(nodes.map(visit), measured, canvasWidth);
    layout.nodes = layout.nodes.flatMap((position) => [position, ...(nested.get(position.node.id)?.nodes ?? []).map((child) => ({
      ...child, x: child.x + position.x - position.width / 2, y: child.y + position.y + 40,
    }))]);
    return layout;
  }
  const independent = independentDeliverables(deliverables);
  const layout = build([...roots, ...independent.map((file) => ({
    id: `deliverable:${file.id}`, deliverable: file, issues: [], collapsed: false, children: [],
  }))]);
  for (const { node } of layout.nodes) {
    if (node.deliverable || node.collapsed || node.id.startsWith("workspace:")) continue;
    if (node.issues[0]?.workspace_id !== wsId) continue;
    for (const file of linkedDeliverables(node.issues[0]?.metadata?.deliverables, independent)) {
      links.push({ source: node.id, target: `deliverable:${file.id}`, kind: "deliverable" });
    }
  }
  return { ...layout, links };
}
