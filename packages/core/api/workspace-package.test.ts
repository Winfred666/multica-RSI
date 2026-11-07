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
  expect(await client.importWorkspacePackage("ws", pkg, selection, false)).toBeNull();
});

it("preserves portable bodies and files while choosing all import resources and no export resources", () => {
  const pkg = WorkspacePackageSchema.parse({ schema_version: 1, projects: [{ id: "p", title: "Project", description: "body" }],
    skills: [{ id: "s", name: "Skill", content: "instructions", files: [{ path: "a.txt", content: "hello" }], is_system_skill: true }],
    plugins: [{ id: "plugin", name: "Plugin", bundle: "zip" }], system_prompt: "prompt", issue_statuses: [{ key: "todo" }] });
  expect(pkg.skills[0]).toMatchObject({ content: "instructions", files: [{ path: "a.txt", content: "hello" }] });
  expect(pkg).toHaveProperty("issue_statuses", [{ key: "todo" }]);
  expect(packageSelection(pkg)).toEqual({ projects_issues: true, agents: [], skills: ["s"], mcps: [], plugins: ["plugin"], system_prompt: true });
  expect(packageSelection()).toEqual({ projects_issues: false, agents: [], skills: [], mcps: [], plugins: [], system_prompt: false });
});
