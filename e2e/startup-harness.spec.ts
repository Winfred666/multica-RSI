import { expect, test } from "@playwright/test";
import { readFileSync } from "node:fs";
import { TestApiClient } from "./fixtures";

test("fresh workspace installs inline workflow and three parked task fixtures", async () => {
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
    expect(saved.context.trim()).toBe(readFileSync("harness/startup.gotmpl", "utf8").trim());
    const skillsResponse = await fetch(`${base}/api/skills`, { headers: workspaceHeaders });
    expect(skillsResponse.ok).toBeTruthy();
    const skills = await skillsResponse.json();
    expect(skills.filter((skill: { is_system_skill: boolean }) => skill.is_system_skill)).toHaveLength(9);
    expect(skills.some((skill: { name: string }) => skill.name === "multica-issue-turn")).toBe(false);

    const fixtures = JSON.parse(readFileSync("harness/testdata/startup-cases.json", "utf8"));
    expect(fixtures).toHaveLength(3);
    for (const fixture of fixtures) {
      const example = fixture.concise_example;
      const issue = await api.createIssue(example.title, { description: example.description, status: "backlog" });
      expect(issue.status).toBe("backlog");
      expect(issue.assignee_id).toBeNull();
      expect(issue.description).toBe(example.description);
    }
  } finally {
    await api.cleanup();
    await api.deleteWorkspace();
  }
});
