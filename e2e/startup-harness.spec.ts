import { expect, test } from "@playwright/test";
import { readFileSync } from "node:fs";
import { TestApiClient } from "./fixtures";

test("fresh workspace imports only the selected startup resources", async () => {
  const api = new TestApiClient();
  await api.login(`startup-harness-${Date.now()}@e2e.test`, "Harness fixture");
  const base = process.env.NEXT_PUBLIC_API_URL!;
  const headers = {
    Authorization: `Bearer ${api.getToken()}`,
    "Content-Type": "application/json",
  };
  const created = await fetch(`${base}/api/workspaces`, {
    method: "POST",
    headers,
    body: JSON.stringify({ name: "Startup fixture", slug: `startup-${Date.now()}` }),
  });
  expect(created.ok).toBeTruthy();
  const workspace = await created.json();
  api.setWorkspaceId(workspace.id);
  try {
    const workspaceHeaders = { ...headers, "X-Workspace-ID": workspace.id };
    const response = await fetch(`${base}/api/workspaces/${workspace.id}`, { headers: workspaceHeaders });
    expect(response.ok).toBeTruthy();
    const saved = await response.json();
    expect(saved.context).toBeNull();
    const skillsResponse = await fetch(`${base}/api/skills`, { headers: workspaceHeaders });
    expect(skillsResponse.ok).toBeTruthy();
    const skills = await skillsResponse.json();
    expect(skills).toHaveLength(0);
    const packageResponse = await fetch(`${base}/api/workspaces/${workspace.id}/package/default`, { headers: workspaceHeaders });
    expect(packageResponse.ok).toBeTruthy();
    const pkg = await packageResponse.json();
    const selection = { projects_issues: false, agents: [], skills: [], mcps: [], plugins: [], system_prompt: false };
    const emptyImport = await fetch(`${base}/api/workspaces/${workspace.id}/import`, {
      method: "POST", headers: workspaceHeaders,
      body: JSON.stringify({ package: pkg, selection }),
    });
    expect(emptyImport.ok).toBeTruthy();
    expect(await emptyImport.json()).toEqual({ projects: 0, issues: 0, agents: 0, skills: 0, mcps: 0, plugins: 0, system_prompt: false });
    const selectedImport = await fetch(`${base}/api/workspaces/${workspace.id}/import`, {
      method: "POST", headers: workspaceHeaders,
      body: JSON.stringify({ package: pkg, selection: { ...selection, skills: [pkg.skills[0].id], system_prompt: true } }),
    });
    expect(selectedImport.ok).toBeTruthy();
    const importedWorkspace = await fetch(`${base}/api/workspaces/${workspace.id}`, { headers: workspaceHeaders });
    expect((await importedWorkspace.json()).context.trim()).toBe(readFileSync("harness/startup.gotmpl", "utf8").trim());
    const importedSkills = await fetch(`${base}/api/skills`, { headers: workspaceHeaders });
    expect(await importedSkills.json()).toHaveLength(1);
  } finally {
    await api.cleanup();
    await api.deleteWorkspace();
  }
});
