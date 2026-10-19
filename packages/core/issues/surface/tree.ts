import type { AgentTask, Issue, TimelineEntry } from "../../types";
import { parseMentions } from "../comment-trigger-outcomes";

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
}

export const TREE_NODE_WIDTH = 300;
export const TREE_NODE_HEIGHT = 96;
export const TREE_LEVEL_HEIGHT = 216;

export function layoutIssueTree(roots: IssueTreeNode[]) {
  const nodes: PositionedIssueTreeNode[] = [];
  let leaf = 0;
  let maxDepth = 0;
  function place(node: IssueTreeNode, depth: number, parentId: string | null): number {
    maxDepth = Math.max(maxDepth, depth);
    const position = { node, x: 0, y: depth * TREE_LEVEL_HEIGHT + 48, parentId };
    nodes.push(position);
    const childXs = node.children.map((child) => place(child, depth + 1, node.id));
    const x = childXs.length
      ? (childXs[0]! + childXs[childXs.length - 1]!) / 2
      : leaf++ * (TREE_NODE_WIDTH + 40) + TREE_NODE_WIDTH / 2 + 48;
    position.x = x;
    return x;
  }
  roots.forEach((root) => place(root, 0, null));
  return {
    nodes,
    width: Math.max(1, leaf) * (TREE_NODE_WIDTH + 40) + 56,
    height: (maxDepth + 1) * TREE_LEVEL_HEIGHT + 48,
  };
}

export function issueTreeParticipants(issue: Issue, timeline: readonly TimelineEntry[], tasks: readonly AgentTask[]) {
  const comments = timeline.filter((entry) => entry.type === "comment" && !entry.deleted_at)
    .toSorted((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
  const replies = comments.filter((entry) => entry.actor_type === "agent" && entry.content?.trim());
  const issueTasks = tasks.filter((task) => task.issue_id === issue.id)
    .toSorted((a, b) => Date.parse(a.started_at ?? a.created_at) - Date.parse(b.started_at ?? b.created_at));
  const primaryId = issue.assignee_type === "agent" ? issue.assignee_id : null;
  const mentions = [issue.description ?? "", ...comments.map((entry) => entry.content ?? "")]
    .flatMap(parseMentions).filter((mention) => mention.type === "agent").map((mention) => mention.id);
  const agentIds = [...new Set([
    ...replies.map((entry) => entry.actor_id),
    ...issueTasks.map((task) => task.agent_id),
    ...(primaryId ? [primaryId] : []),
    ...mentions,
  ])];
  return {
    agentIds,
    primaryId,
    runningIds: new Set(issueTasks.filter((task) => task.status === "running").map((task) => task.agent_id)),
    report: replies[0],
  };
}
