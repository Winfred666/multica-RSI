import type { Attachment, Issue, TimelineEntry } from "../../types";

export interface IssueTreeNode {
  id: string;
  issues: Issue[];
  collapsed: boolean;
  deliverable?: Attachment;
  children: IssueTreeNode[];
}

/** Only connect issues in the filtered window; never fetch missing relatives. */
export function buildIssueTree(
  issues: readonly Issue[],
  hiddenStatuses: ReadonlySet<string>,
  expandedGroups: ReadonlySet<string>,
): IssueTreeNode[] {
  const byId = new Map(issues.map((issue) => [issue.id, issue]));
  const children = new Map<string, Issue[]>();
  for (const issue of byId.values()) {
    if (!issue.parent_issue_id || issue.parent_issue_id === issue.id) continue;
    const siblings = children.get(issue.parent_issue_id) ?? [];
    siblings.push(issue);
    children.set(issue.parent_issue_id, siblings);
  }
  const visited = new Set<string>();
  function visit(issue: Issue): IssueTreeNode[] {
    if (visited.has(issue.id)) return [];
    visited.add(issue.id);
    return [{
      id: issue.id,
      issues: [issue],
      collapsed: false,
      children: (children.get(issue.id) ?? []).flatMap(visit),
    }];
  }
  const roots = [...byId.values()]
    .filter((issue) => !issue.parent_issue_id || !byId.has(issue.parent_issue_id) || issue.parent_issue_id === issue.id)
    .flatMap(visit);
  // Malformed cyclic relations still render each issue once.
  for (const issue of byId.values()) roots.push(...visit(issue));

  function collapse(nodes: IssueTreeNode[], parent: string): IssueTreeNode[] {
    const byStatus = new Map<string, IssueTreeNode[]>();
    for (const node of nodes) {
      const status = node.issues[0]!.status;
      if (!hiddenStatuses.has(status)) continue;
      const group = byStatus.get(status) ?? [];
      group.push(node);
      byStatus.set(status, group);
    }
    return nodes.flatMap((node) => {
      const status = node.issues[0]!.status;
      const groupId = `collapsed:${parent}:${status}`;
      const hidden = byStatus.get(status);
      if (!hidden || hidden.length < 2 || expandedGroups.has(groupId)) {
        return [{ ...node, children: collapse(node.children, node.id) }];
      }
      return node === hidden[0] ? [{
        id: groupId,
        issues: hidden.flatMap((entry) => entry.issues),
        collapsed: true,
        children: [],
      }] : [];
    });
  }
  return collapse(roots, "root");
}

export interface PositionedIssueTreeNode {
  node: IssueTreeNode;
  x: number;
  y: number;
  parentId: string | null;
  height: number;
  width: number;
}

export interface IssueTreeSize { width: number; height: number }

export const TREE_NODE_WIDTH = 300;
export const TREE_NODE_HEIGHT = 96;
export function layoutIssueTree(roots: IssueTreeNode[], sizes: ReadonlyMap<string, IssueTreeSize> = new Map(), canvasWidth = 1200) {
  const nodes: PositionedIssueTreeNode[] = [];
  const bounds = new Map<string, IssueTreeSize>();
  function measure(node: IssueTreeNode): IssueTreeSize {
    const children = node.children.map(measure);
    const size = {
      width: Math.max(sizes.get(node.id)?.width ?? TREE_NODE_WIDTH,
        children.reduce((sum, child) => sum + child.width, 0) + Math.max(0, children.length - 1) * 40),
      height: (sizes.get(node.id)?.height ?? TREE_NODE_HEIGHT) +
        (children.length ? 64 + Math.max(...children.map((child) => child.height)) : 0),
    };
    bounds.set(node.id, size);
    return size;
  }
  roots.forEach(measure);
  function place(node: IssueTreeNode, y: number, parentId: string | null, left: number) {
    const height = sizes.get(node.id)?.height ?? TREE_NODE_HEIGHT;
    const width = sizes.get(node.id)?.width ?? TREE_NODE_WIDTH;
    const subtreeWidth = bounds.get(node.id)!.width;
    nodes.push({ node, x: left + subtreeWidth / 2, y, parentId, height, width });
    const childrenWidth = node.children.reduce((sum, child) => sum + bounds.get(child.id)!.width, 0) + Math.max(0, node.children.length - 1) * 40;
    let childLeft = left + (subtreeWidth - childrenWidth) / 2;
    for (const child of node.children) {
      place(child, y + height + 64, node.id, childLeft);
      childLeft += bounds.get(child.id)!.width + 40;
    }
  }
  // TODO: only temp auto tree layout. Future draggable would store custom layout model, which might require backend change.
  // Pack whole trees in order; their bounding boxes keep descendants apart.
  const rowWidth = Math.max(TREE_NODE_WIDTH * 2 + 40, canvasWidth - 96);
  let left = 48, top = 48, rowHeight = 0, right = 48, bottom = 48;
  for (const root of roots) {
    const size = bounds.get(root.id)!;
    if (left > 48 && left + size.width > rowWidth + 48) {
      left = 48;
      top += rowHeight + 40;
      rowHeight = 0;
    }
    place(root, top, null, left);
    right = Math.max(right, left + size.width);
    bottom = Math.max(bottom, top + size.height);
    rowHeight = Math.max(rowHeight, size.height);
    left += size.width + 40;
  }
  return {
    nodes,
    width: Math.max(TREE_NODE_WIDTH + 96, right + 48),
    height: Math.max(TREE_NODE_HEIGHT + 96, bottom + 48),
  };
}

export interface IssueTreeCollaborator {
  actorType: "member" | "agent";
  actorId: string;
  messages: TimelineEntry[];
}

export function issueTreeCollaborators(timeline: readonly TimelineEntry[]) {
  const collaborators = new Map<string, IssueTreeCollaborator>();
  for (const entry of timeline.toSorted((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))) {
    if (entry.type !== "comment" || entry.deleted_at ||
      (entry.actor_type !== "member" && entry.actor_type !== "agent") ||
      entry.comment_type === "system" || entry.comment_type === "status_change" ||
      (!entry.content?.trim() && !entry.attachments?.length)) continue;
    const key = `${entry.actor_type}:${entry.actor_id}`;
    const collaborator = collaborators.get(key) ?? { actorType: entry.actor_type, actorId: entry.actor_id, messages: [] };
    collaborator.messages.push(entry);
    collaborators.set(key, collaborator);
  }
  return [...collaborators.values()].sort((a, b) => b.messages.length - a.messages.length ||
    Date.parse(b.messages[0]!.created_at) - Date.parse(a.messages[0]!.created_at));
}
