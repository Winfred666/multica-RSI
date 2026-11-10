import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { WorkspacePackage, WorkspacePackageCategory, WorkspacePackageItem, WorkspacePackageSelection } from "../api/schemas";
import { workspaceKeys } from "./queries";

export const packageInventoryOptions = (wsId: string) => queryOptions({
  queryKey: ["workspaces", wsId, "package"],
  queryFn: () => api.getWorkspacePackageInventory(wsId),
});

export const defaultPackageOptions = (wsId: string) => queryOptions({
  queryKey: ["workspaces", wsId, "package", "default"],
  queryFn: () => api.getDefaultWorkspacePackage(wsId),
});

export const packageItemsOptions = (wsId: string, category: WorkspacePackageCategory) => queryOptions({
  queryKey: ["workspaces", wsId, "package", category],
  staleTime: 30_000,
  queryFn: async () => {
    const items: WorkspacePackageItem[] = [];
    let total = 0;
    do {
      const page = await api.getWorkspacePackageInventory(wsId, category, items.length);
      if (!page || (!page.items.length && items.length < page.counts[category])) throw new Error("Invalid package inventory");
      items.push(...page.items);
      total = page.counts[category];
    } while (items.length < total);
    return items;
  },
});

export function packageSelection(pkg?: WorkspacePackage): WorkspacePackageSelection {
  return {
    projects: pkg?.projects.map((item) => item.id) ?? [],
    issues: pkg?.issues.map((item) => item.id) ?? [],
    agents: pkg?.agents.map((item) => item.id) ?? [],
    skills: pkg?.skills.map((item) => item.id) ?? [],
    mcps: pkg?.mcps.map((item) => item.id) ?? [],
    plugins: pkg?.plugins.map((item) => item.id) ?? [],
    system_prompt: pkg?.system_prompt != null,
  };
}

export function useImportWorkspacePackage(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ pkg, selection, pluginExecutionToken }: { pkg: WorkspacePackage; selection?: WorkspacePackageSelection; pluginExecutionToken?: string }) => {
      const result = await api.importWorkspacePackage(wsId, pkg, selection, pluginExecutionToken);
      if (!result) throw new Error("Invalid workspace import response");
      return result;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ predicate: (query) => query.queryKey.includes(wsId) });
      void qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}
