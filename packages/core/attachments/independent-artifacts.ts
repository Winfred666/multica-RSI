import { useQueries } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { independentArtifactIds } from "./deliverables";

export function useIndependentArtifacts(value: unknown, enabled = true) {
  const wsId = useWorkspaceId();
  return useQueries({
    queries: independentArtifactIds(value).map((id) => ({
      queryKey: ["attachment-metadata", id, wsId],
      queryFn: () => api.getAttachment(id),
      enabled,
      retry: false,
    })),
    combine: (results) => results.flatMap((result) => !result.isError && result.data?.id ? [result.data] : []),
  });
}
