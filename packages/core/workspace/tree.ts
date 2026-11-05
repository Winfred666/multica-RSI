import { queryOptions } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import type { Issue } from "../types";

export function issueWorkspaceId(issue: Pick<Issue, "metadata">): string | null {
  const id = issue.metadata?.sub_workspace_id;
  return typeof id === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id)
    ? id : null;
}

export interface WorkspaceTree {
  workspace: { id: string; slug: string; name: string };
  issues: Issue[];
}

export function workspaceTreeOptions(wsId: string) {
  return queryOptions({
    queryKey: ["workspaces", wsId, "tree"] as const,
    queryFn: async (): Promise<WorkspaceTree> => {
      const workspace = await api.getWorkspaceTreeIdentity(wsId);
      const issues: Issue[] = [];
      while (true) {
        const page = await api.listIssues({ workspace_id: wsId, offset: issues.length, limit: 100 }, workspace.slug);
        issues.push(...page.issues);
        if (issues.length >= page.total) break;
        if (page.issues.length === 0) throw new Error("Workspace issue pagination made no progress");
      }
      return { workspace, issues };
    },
    retry: false,
    refetchInterval: (query) => query.state.error instanceof ApiError &&
      [403, 404].includes(query.state.error.status) ? false : 10_000,
  });
}
