"use client";

import { useState } from "react";
import { useIsFetching, useMutation, useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { packageInventoryOptions, packageSelection } from "@multica/core/workspace";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogTrigger, DialogContent, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { PackageChecklist } from "./package-checklist";
import { WorkspaceImport } from "./workspace-import";
import { useT } from "../i18n";

function WorkspaceExport({ wsId }: { wsId: string }) {
  const { t } = useT("workspace");
  const inventory = useQuery(packageInventoryOptions(wsId));
  const fetching = useIsFetching({ queryKey: ["workspaces", wsId, "package"] });
  const [selection, setSelection] = useState(() => packageSelection());
  const mutation = useMutation({
    mutationFn: async () => {
      const pkg = await api.exportWorkspacePackage(wsId, selection);
      if (!pkg) throw new Error(t(($) => $.package.failed));
      const url = URL.createObjectURL(new Blob([JSON.stringify(pkg, null, 2)], { type: "application/json" }));
      const link = document.createElement("a");
      link.href = url;
      link.download = "workspace-package.json";
      link.click();
      URL.revokeObjectURL(url);
    },
  });
  return <div className="space-y-4">
    {inventory.isPending && <p role="status">{t(($) => $.package.loading)}</p>}
    {(inventory.isError || inventory.data === null) && <p role="alert" className="text-destructive">{t(($) => $.package.failed)}</p>}
    {inventory.data && <>
      <PackageChecklist wsId={wsId} counts={inventory.data.counts} selection={selection} onChange={setSelection} disabled={mutation.isPending} />
      {mutation.isError && <p role="alert" className="text-caption text-destructive">{mutation.error.message}</p>}
      <Button disabled={mutation.isPending || fetching > 0} aria-busy={mutation.isPending} onClick={() => mutation.mutate()}>{t(($) => $.package.export_action)}</Button>
    </>}
  </div>;
}

export function WorkspaceTransfer({ wsId, action }: { wsId: string; action: "import_action" | "export_action" }) {
  const { t } = useT("workspace");
  const [busy, setBusy] = useState(false);
  return <Dialog onOpenChange={(_, event) => { if (busy) event.cancel(); }}>
    <DialogTrigger render={<Button variant="outline" />}>{t(($) => $.package[action])}</DialogTrigger>
    <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-lg" showCloseButton>
      <DialogHeader><DialogTitle>{t(($) => $.package[action])}</DialogTitle></DialogHeader>
      {action === "import_action" ? <WorkspaceImport wsId={wsId} onBusyChange={setBusy} /> : <WorkspaceExport wsId={wsId} />}
    </DialogContent>
  </Dialog>;
}
