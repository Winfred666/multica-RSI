// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient, setApiInstance } from "../api";
import { setCurrentWorkspace, getCurrentSlug } from "../platform/workspace-storage";
import { workspaceTreeOptions } from "./tree";

const row = (id: string) => ({ id, workspace_id: "child", number: 1, identifier: "P-1", title: id, description: null, status: "todo", priority: "none", assignee_type: null, assignee_id: null, creator_type: "member", creator_id: "owner", parent_issue_id: null, project_id: null, position: 0, start_date: null, due_date: null, created_at: "2026-01-01", updated_at: "2026-01-01" });
const id = "0c99f8e3-543b-4394-bfe8-a5b60141352e";
afterEach(() => { vi.unstubAllGlobals(); setCurrentWorkspace(null, null); });

it("fetches every child-workspace page with explicit scope while preserving the parent context", async () => {
  setCurrentWorkspace("parent", "parent-id");
  const fetch = vi.fn()
    .mockResolvedValueOnce(Response.json({ id, slug: "playground", name: "Playground" }))
    .mockResolvedValueOnce(Response.json({ issues: [row("one")], total: 2 }))
    .mockResolvedValueOnce(Response.json({ issues: [row("two")], total: 2 }));
  vi.stubGlobal("fetch", fetch);
  setApiInstance(new ApiClient("https://api.example.test"));
  const options = workspaceTreeOptions(id);
  const result = await new QueryClient().fetchQuery(options);
  expect(result.issues.map((issue) => issue.id)).toEqual(["one", "two"]);
  expect(fetch.mock.calls[0]![1].headers).toMatchObject({ "X-Workspace-ID": id, "X-Workspace-Slug": "" });
  for (const [, init] of fetch.mock.calls.slice(1)) expect(init.headers["X-Workspace-Slug"]).toBe("playground");
  expect(fetch.mock.calls[2]![0]).toContain("offset=1");
  expect(getCurrentSlug()).toBe("parent");
});

it("rejects malformed workspace identity before requesting issues", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json({ id: 42, slug: "playground" }));
  vi.stubGlobal("fetch", fetch);
  await expect(new ApiClient("https://api.example.test").getWorkspaceTreeIdentity(id)).rejects.toThrow("Invalid workspace identity");
  expect(fetch).toHaveBeenCalledOnce();
});
