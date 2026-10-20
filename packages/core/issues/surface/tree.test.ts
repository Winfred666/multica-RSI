// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Attachment, Issue, TimelineEntry } from "../../types";
import { buildIssueTree, issueTreeCollaborators, layoutIssueTree } from "./tree";

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

  it("spaces levels for the tallest configurable card and updates when it shrinks", () => {
    const roots = buildIssueTree([issue("p"), issue("sibling"), issue("child", "p")], new Set(), new Set());
    const layout = layoutIssueTree(roots, new Map([["p", 450], ["sibling", 520]]));
    const parent = layout.nodes.find((node) => node.node.id === "p")!;
    const child = layout.nodes.find((node) => node.node.id === "child")!;
    expect(child.y - parent.y).toBe(520 + 64);
    expect(parent.height).toBe(450);
    expect(layoutIssueTree(roots).height).toBeLessThan(layout.height);
  });

  it("ranks actual collaborators by message count, then recency, and keeps each history newest first", () => {
    const comment = (id: string, actor_type: string, actor_id: string, date: string): TimelineEntry => ({
      id, type: "comment", actor_type, actor_id, content: `Message ${id}`, created_at: date,
    });
    const timeline: TimelineEntry[] = [
      comment("old", "member", "ceo", "2026-01-01"),
      comment("new", "member", "ceo", "2026-01-03"),
      comment("agent", "agent", "worker", "2026-01-04"),
      comment("earlier", "member", "reviewer", "2026-01-02"),
      comment("system", "system", "platform", "2026-01-05"),
      { ...comment("deleted", "member", "deleted", "2026-01-05"), deleted_at: "2026-01-06" },
      { ...comment("status", "member", "status-only", "2026-01-05"), comment_type: "status_change" },
      { ...comment("activity", "member", "activity-only", "2026-01-05"), type: "activity" },
      { ...comment("empty", "member", "empty", "2026-01-05"), content: " " },
    ];
    const result = issueTreeCollaborators(timeline);
    expect(result.map((actor) => actor.actorId)).toEqual(["ceo", "worker", "reviewer"]);
    expect(result[0]!.messages.map((entry) => entry.id)).toEqual(["new", "old"]);
    expect(result[0]!.actorType).toBe("member");
    expect(issueTreeCollaborators([])).toEqual([]);
  });

  it("keeps attachment-only contributions and separates a human and agent with the same id", () => {
    const entry: TimelineEntry = {
      id: "upload", type: "comment", actor_type: "member", actor_id: "same-id", created_at: "2026-01-01",
      content: "", attachments: [{ id: "file", filename: "reference.md" } as Attachment],
    };
    const result = issueTreeCollaborators([entry, { ...entry, id: "reply", actor_type: "agent", content: "Reviewed" }]);
    expect(result.map((actor) => actor.actorType)).toEqual(["member", "agent"]);
    expect(result[0]!.messages[0]!.id).toBe("upload");
  });
});
