// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { WorkspacePackageSchema } from "./schemas";
import { packageSelection } from "../workspace/packages";

afterEach(() => vi.unstubAllGlobals());

it("rejects malformed package responses at each API boundary", async () => {
  vi.stubGlobal("fetch", vi.fn().mockImplementation(async () => new Response(JSON.stringify({ agents: "bad" }), { headers: { "Content-Type": "application/json" } })));
  const client = new ApiClient("https://api.example.test");
  const pkg = WorkspacePackageSchema.parse({ schema_version: 1 });
  const selection = packageSelection();
  expect(await client.getWorkspacePackageInventory("ws")).toBeNull();
  expect(await client.getDefaultWorkspacePackage("ws")).toBeNull();
  expect(await client.inspectWorkspacePackage("ws", pkg)).toBeNull();
  expect(await client.exportWorkspacePackage("ws", selection)).toBeNull();
  expect(await client.importWorkspacePackage("ws", pkg, selection)).toBeNull();
});

it("preserves portable bodies and files while choosing all import resources and no export resources", () => {
  const pkg = WorkspacePackageSchema.parse({ schema_version: 1, projects: [{ id: "p", title: "Project", description: "body" }],
    issues: [{ id: "i", title: "Issue", project_id: "p", parent_id: "parent" }],
    skills: [{ id: "s", name: "Skill", content: "instructions", files: [{ path: "a.txt", content: "hello" }], is_system_skill: true }],
    plugins: [{ id: "plugin", name: "Plugin", bundle: "zip" }], system_prompt: "prompt", issue_statuses: [{ key: "todo" }] });
  expect(pkg.skills[0]).toMatchObject({ content: "instructions", files: [{ path: "a.txt", content: "hello" }] });
  expect(pkg).toHaveProperty("issue_statuses", [{ key: "todo" }]);
  expect(packageSelection(pkg)).toEqual({ projects: ["p"], issues: ["i"], agents: [], skills: ["s"], mcps: [], plugins: ["plugin"], system_prompt: true });
  expect(packageSelection()).toEqual({ projects: [], issues: [], agents: [], skills: [], mcps: [], plugins: [], system_prompt: false });
});

it("sends ID arrays for export, inspect and import, preserving omitted import selections", async () => {
  const pkg = WorkspacePackageSchema.parse({ schema_version: 1 });
  const counts = { projects: 0, issues: 0, agents: 0, skills: 0, mcps: 0, plugins: 0, system_prompt: false };
  const fetch = vi.fn().mockImplementation(async (url: string) => new Response(JSON.stringify(
    url.endsWith("/export") ? pkg : url.endsWith("/import") ? counts : { counts, items: [] },
  ), { headers: { "Content-Type": "application/json" } }));
  vi.stubGlobal("fetch", fetch);
  const client = new ApiClient("https://api.example.test");
  const selection = { ...packageSelection(), projects: ["4b75bde2-3dd5-4db1-bf7b-8548d1f9ba61"], issues: ["32fc318f-bc8a-4747-a5a0-7e4ac88d9704"] };

  await client.exportWorkspacePackage("ws", selection);
  await client.inspectWorkspacePackage("ws", pkg, selection);
  await client.importWorkspacePackage("ws", pkg, selection);
  await client.inspectWorkspacePackage("ws", pkg);
  await client.importWorkspacePackage("ws", pkg);
  expect(fetch.mock.calls.map(([, init]) => JSON.parse(init.body))).toEqual([
    selection, { package: pkg, selection }, { package: pkg, selection }, { package: pkg }, { package: pkg },
  ]);
});
