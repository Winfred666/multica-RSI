// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Attachment, Issue, TimelineEntry } from "../../types";
import { buildIssueTree, issueTreeCollaborators, layoutIssueTree } from "./tree";

const issue = (id: string, parent: string | null = null, status = "todo"): Issue => ({
  id, parent_issue_id: parent, status, assignee_type: "agent", assignee_id: `agent-${id}`,
} as Issue);

describe("issue tree", () => {
  it("places a wide workspace frame beside ordinary children without overlap", () => {
    const roots = buildIssueTree([issue("parent"), issue("workspace", "parent"), issue("child", "parent")], new Set(), new Set());
    const layout = layoutIssueTree(roots, new Map([["workspace", { width: 760, height: 700 }]]));
    const parent = layout.nodes.find(({ node }) => node.id === "parent")!;
    const workspace = layout.nodes.find(({ node }) => node.id === "workspace")!;
    const child = layout.nodes.find(({ node }) => node.id === "child")!;
    expect(child.x - child.width / 2).toBeGreaterThan(workspace.x + workspace.width / 2);
    expect(child.y).toBe(workspace.y);
    expect(workspace.y).toBeGreaterThan(parent.y + parent.height);
    expect(layout.width).toBeGreaterThan(child.x + child.width / 2);
  });
  it("keeps filtered roots and sibling order, without inventing missing parents", () => {
    const roots = buildIssueTree([issue("b", "parent"), issue("a", "parent"), issue("parent"), issue("orphan", "outside")], new Set(), new Set());
    expect(roots.map((node) => node.id)).toEqual(["parent", "orphan"]);
    expect(roots[0]!.children.map((node) => node.id)).toEqual(["b", "a"]);
    const layout = layoutIssueTree(roots);
    expect(layout.nodes.find((node) => node.node.id === "b")!.x).toBeLessThan(layout.nodes.find((node) => node.node.id === "a")!.x);
    expect(layout.nodes.find((node) => node.node.id === "parent")!.y).toBeLessThan(layout.nodes.find((node) => node.node.id === "b")!.y);
  });

  it("collapses hidden siblings with all their descendants, then restores them on expansion", () => {
    const issues = [issue("p"), issue("done", "p", "done"), issue("cancel", "p", "cancelled"), issue("done2", "p", "done"), issue("cancel2", "p", "cancelled"), issue("active", "done"), issue("grandchild", "active"), issue("visible", "p")];
    const hidden = new Set(["done", "cancelled"]);
    const groups = buildIssueTree(issues, hidden, new Set())[0]!.children;
    const group = groups[0]!;
    expect(group.collapsed).toBe(true);
    expect(group.issues.map((row) => row.id)).toEqual(["done", "done2"]);
    expect(groups[1]!.issues.map((row) => row.id)).toEqual(["cancel", "cancel2"]);
    expect(group.children).toEqual([]);
    expect(layoutIssueTree(buildIssueTree(issues, hidden, new Set())).nodes.map(({ node }) => node.id)).toEqual(["p", group.id, groups[1]!.id, "visible"]);
    expect(layoutIssueTree(buildIssueTree(issues, hidden, new Set([group.id]))).nodes.map(({ node }) => node.id)).toEqual(["p", "done", "active", "grandchild", groups[1]!.id, "done2", "visible"]);
  });

  it("renders cycles and duplicate rows once", () => {
    const issues = [issue("a", "b"), issue("b", "a"), issue("self", "self"), issue("a", "b"), issue("outside", "missing"), { ...issue("member", "a"), assignee_type: "member" as const }];
    const { nodes } = layoutIssueTree(buildIssueTree(issues, new Set(), new Set()));
    expect(nodes).toHaveLength(5);
    expect(new Set(nodes.map((node) => node.node.id)).size).toBe(5);
  });

  it("keeps single hidden siblings and their descendants expanded", () => {
    const issues = [issue("root", null, "done"), issue("parent", "root", "done"), issue("child", "parent")];
    const { nodes } = layoutIssueTree(buildIssueTree(issues, new Set(["done"]), new Set()));
    expect(nodes.map(({ node }) => node.id)).toEqual(["root", "parent", "child"]);
    expect(nodes.every(({ node }) => !node.collapsed)).toBe(true);
  });

  it("places children below their own parent regardless of taller neighboring cards", () => {
    const roots = buildIssueTree([issue("root"), issue("p", "root"), issue("sibling", "root"), issue("child", "p"), issue("other-child", "sibling")], new Set(), new Set());
    const layout = layoutIssueTree(roots, new Map([["p", { width: 300, height: 450 }], ["sibling", { width: 300, height: 520 }]]));
    const parent = layout.nodes.find((node) => node.node.id === "p")!;
    const child = layout.nodes.find((node) => node.node.id === "child")!;
    expect(child.y - parent.y).toBe(450 + 64);
    expect(layout.nodes.find(({ node }) => node.id === "other-child")!.y).toBeGreaterThan(child.y);
    expect(parent.height).toBe(450);
    expect(layoutIssueTree(roots).height).toBeLessThan(layout.height);
  });

  it("wraps root bounding boxes to the canvas width without overlapping their descendants", () => {
    const rows = Array.from({ length: 6 }, (_, i) => [issue(`root-${i}`), issue(`child-${i}`, `root-${i}`)]).flat();
    const roots = buildIssueTree(rows, new Set(), new Set());
    const wide = layoutIssueTree(roots, new Map(), 1200);
    const narrow = layoutIssueTree(roots, new Map(), 390);
    const rootRows = (layout: typeof wide) => new Set(layout.nodes.filter(({ parentId }) => !parentId).map(({ y }) => y)).size;
    expect(rootRows(wide)).toBe(2);
    expect(rootRows(narrow)).toBe(3);
    for (const layout of [wide, narrow]) {
      for (const [index, a] of layout.nodes.entries()) {
        expect(a.x - a.width / 2).toBeGreaterThanOrEqual(0);
        expect(a.x + a.width / 2).toBeLessThanOrEqual(layout.width);
        expect(a.y + a.height).toBeLessThanOrEqual(layout.height);
        for (const b of layout.nodes.slice(index + 1)) {
          const overlaps = Math.abs(a.x - b.x) < (a.width + b.width) / 2 && a.y < b.y + b.height && b.y < a.y + a.height;
          expect(overlaps, `${a.node.id} overlaps ${b.node.id}`).toBe(false);
        }
      }
    }
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
