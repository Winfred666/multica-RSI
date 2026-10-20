import type { Issue, TimelineEntry } from "../../types";

export interface IssueTreeNode {
  id: string;
  issues: Issue[];
  collapsed: boolean;
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
    const groupId = `collapsed:${parent}`;
    const hidden = nodes.filter((node) => hiddenStatuses.has(node.issues[0]!.status));
    if (hidden.length === 0 || expandedGroups.has(groupId)) {
      return nodes.map((node) => ({ ...node, children: collapse(node.children, node.id) }));
    }
    const hiddenIds = new Set(hidden.map((node) => node.id));
    const group: IssueTreeNode = {
      id: groupId,
      issues: hidden.flatMap((node) => node.issues),
      collapsed: true,
      // Keep active descendants reachable even when their parent is hidden.
      children: collapse(hidden.flatMap((node) => node.children), groupId),
    };
    return nodes.flatMap((node) => hiddenIds.has(node.id)
      ? node === hidden[0] ? [group] : []
      : [{ ...node, children: collapse(node.children, node.id) }]);
  }
  return collapse(roots, "root");
}

export interface PositionedIssueTreeNode {
  node: IssueTreeNode;
  x: number;
  y: number;
  parentId: string | null;
  height: number;
}

export const TREE_NODE_WIDTH = 300;
export const TREE_NODE_HEIGHT = 96;
export const TREE_LEVEL_HEIGHT = 216;

export function layoutIssueTree(roots: IssueTreeNode[], heights: ReadonlyMap<string, number> = new Map()) {
  const nodes: PositionedIssueTreeNode[] = [];
  let leaf = 0;
  const levels: number[] = [];
  function place(node: IssueTreeNode, depth: number, parentId: string | null): number {
    const height = heights.get(node.id) ?? TREE_NODE_HEIGHT;
    levels[depth] = Math.max(levels[depth] ?? TREE_LEVEL_HEIGHT, height + 64);
    const position = { node, x: 0, y: depth, parentId, height };
    nodes.push(position);
    const childXs = node.children.map((child) => place(child, depth + 1, node.id));
    const x = childXs.length
      ? (childXs[0]! + childXs[childXs.length - 1]!) / 2
      : leaf++ * (TREE_NODE_WIDTH + 40) + TREE_NODE_WIDTH / 2 + 48;
    position.x = x;
    return x;
  }
  roots.forEach((root) => place(root, 0, null));
  let height = 48;
  const tops = levels.map((level) => {
    const top = height;
    height += level;
    return top;
  });
  for (const position of nodes) position.y = tops[position.y]!;
  return {
    nodes,
    width: Math.max(1, leaf) * (TREE_NODE_WIDTH + 40) + 56,
    height: Math.max(TREE_LEVEL_HEIGHT + 48, height),
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
