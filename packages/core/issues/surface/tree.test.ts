// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AgentTask, Issue, TimelineEntry } from "../../types";
import { buildIssueTree, issueTreeParticipants, layoutIssueTree } from "./tree";

const issue = (id: string, parent: string | null = null, status = "todo"): Issue => ({
  id, parent_issue_id: parent, status, assignee_type: "agent", assignee_id: `agent-${id}`,
} as Issue);

describe("issue tree", () => {
  it("keeps filtered roots and sibling order, without inventing missing parents", () => {
    const roots = buildIssueTree([issue("b", "parent"), issue("a", "parent"), issue("parent"), issue("orphan", "outside")], new Set(), new Set());
    expect(roots.map((node) => node.id)).toEqual(["parent", "orphan"]);
    expect(roots[0]!.children.map((node) => node.id)).toEqual(["b", "a"]);
    const layout = layoutIssueTree(roots);
    expect(layout.nodes.find((node) => node.node.id === "b")!.x).toBeLessThan(layout.nodes.find((node) => node.node.id === "a")!.x);
    expect(layout.nodes.find((node) => node.node.id === "parent")!.y).toBeLessThan(layout.nodes.find((node) => node.node.id === "b")!.y);
  });

  it("collapses hidden siblings together while preserving active descendants, then expands", () => {
    const issues = [issue("p"), issue("done", "p", "done"), issue("cancel", "p", "cancelled"), issue("active", "done")];
    const hidden = new Set(["done", "cancelled"]);
    const group = buildIssueTree(issues, hidden, new Set())[0]!.children[0]!;
    expect(group.collapsed).toBe(true);
    expect(group.issues.map((row) => row.id)).toEqual(["done", "cancel"]);
    expect(group.children[0]!.id).toBe("active");
    expect(buildIssueTree(issues, hidden, new Set([group.id]))[0]!.children.map((node) => node.id)).toEqual(["done", "cancel"]);
  });

  it("renders cycles and duplicate rows once", () => {
    const issues = [issue("a", "b"), issue("b", "a"), issue("self", "self"), issue("a", "b"), issue("outside", "missing"), { ...issue("member", "a"), assignee_type: "member" as const }];
    const { nodes } = layoutIssueTree(buildIssueTree(issues, new Set(), new Set()));
    expect(nodes).toHaveLength(5);
    expect(new Set(nodes.map((node) => node.node.id)).size).toBe(5);
  });

  it("orders collaborators by latest reply, deduplicates mentions, and glows every running agent", () => {
    const row = { ...issue("x"), description: "[@Helper](mention://agent/aaaa)" };
    const comment = (id: string, actor: string, date: string, deleted_at?: string): TimelineEntry => ({
      id, type: "comment", actor_type: "agent", actor_id: actor, content: `Report ${id}`, created_at: date, deleted_at,
    });
    const timeline = [comment("old", "aaaa", "2026-01-01"), comment("new", "bbbb", "2026-01-03"), comment("deleted", "cccc", "2026-01-04", "2026-01-05")];
    const tasks = ["aaaa", "bbbb"].map((agent_id) => ({ agent_id, issue_id: "x", status: "running", created_at: "2026-01-02", started_at: null } as AgentTask));
    const result = issueTreeParticipants(row, timeline, tasks);
    expect(result.agentIds).toEqual(["bbbb", "aaaa", "agent-x"]);
    expect(result.runningIds).toEqual(new Set(["aaaa", "bbbb"]));
    expect(result.report?.id).toBe("new");
    expect(result.primaryId).toBe("agent-x");
  });
});
