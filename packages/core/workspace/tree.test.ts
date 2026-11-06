// @vitest-environment node
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient, setApiInstance } from "../api";
import { setCurrentWorkspace, getCurrentSlug } from "../platform/workspace-storage";
import type { Issue } from "../types";
import { buildIssueTree } from "../issues/surface/tree";
import { layoutWorkspaceTree, workspaceTreeOptions } from "./tree";

const child = "0c99f8e3-543b-4394-bfe8-a5b60141352e";
const nested = "1c99f8e3-543b-4394-bfe8-a5b60141352e";
const row = (id: string, workspace_id: string, target?: string): Issue => ({ id, workspace_id, metadata: target ? { sub_workspace_id: target } : {}, number: 1, identifier: "P-1", title: id, description: null, status: "todo", stage: null, properties: {}, priority: "none", assignee_type: null, assignee_id: null, creator_type: "member", creator_id: "owner", parent_issue_id: null, project_id: null, position: 0, start_date: null, due_date: null, created_at: "2026-01-01", updated_at: "2026-01-01" });
afterEach(() => { vi.unstubAllGlobals(); setCurrentWorkspace(null, null); });

it("captures each workspace once, paginates with explicit scope, and renders shared and cyclic references once", async () => {
  setCurrentWorkspace("parent", "parent-id");
  const fetch = vi.fn()
    .mockResolvedValueOnce(Response.json([{ id: child, slug: "playground", name: "Playground", created_at: "2026-01-01" }, { id: nested, slug: "nested", name: "Nested", created_at: "2026-01-01" }]))
    .mockResolvedValueOnce(Response.json({ issues: [row("one", child, nested)], total: 2 }))
    .mockResolvedValueOnce(Response.json({ issues: [row("two", child)], total: 2 }))
    .mockResolvedValueOnce(Response.json({ issues: [row("three", nested, child)], total: 1 }));
  vi.stubGlobal("fetch", fetch);
  setApiInstance(new ApiClient("https://api.example.test"));
  const roots = [row("owner", "parent-id", child), row("other-owner", "parent-id", child), { ...row("sibling", "parent-id"), parent_issue_id: "owner" }];
  const client = new QueryClient();
  const options = workspaceTreeOptions("parent-id", roots);
  const observer = new QueryObserver(client, options);
  const unsubscribe = observer.subscribe(() => {});
  await vi.waitFor(() => expect(observer.getCurrentResult().isSuccess).toBe(true));
  const snapshots = observer.getCurrentResult().data!;
  expect(snapshots[child]!.error).toBeUndefined();
  expect(snapshots[child]!.issues.map((issue) => issue.id)).toEqual(["one", "two"]);
  expect(fetch.mock.calls.slice(1).map(([, init]) => init.headers["X-Workspace-Slug"])).toEqual(["playground", "playground", "nested"]);
  expect(fetch.mock.calls[2]![0]).toContain("offset=1");
  expect(getCurrentSlug()).toBe("parent");
  await client.invalidateQueries();
  unsubscribe();
  const remount = new QueryObserver(client, options).subscribe(() => {});
  expect(fetch).toHaveBeenCalledTimes(4);
  const layout = layoutWorkspaceTree(buildIssueTree(roots, new Set(), new Set()), "parent-id", snapshots);
  expect(layout.nodes.map(({ node }) => node.id)).toEqual(expect.arrayContaining(["owner", "other-owner", "one", "two", "three", `workspace:${child}`, `workspace:${nested}`]));
  expect(new Set(layout.nodes.map(({ node }) => node.id)).size).toBe(layout.nodes.length);
  expect(layout.links).toEqual([{ source: "three", target: `workspace:${child}` }, { source: "other-owner", target: `workspace:${child}` }]);
  const outer = layout.nodes.find(({ node }) => node.id === `workspace:${child}`)!;
  const inner = layout.nodes.find(({ node }) => node.id === `workspace:${nested}`)!;
  const sibling = layout.nodes.find(({ node }) => node.id === "sibling")!;
  expect(sibling.x + sibling.width / 2).toBeLessThan(outer.x - outer.width / 2);
  expect(inner.y + inner.height).toBeLessThan(outer.y + outer.height);
  remount();
  client.clear();
});
