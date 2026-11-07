"use client";

import { useCallback, useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api, parseWithFallback } from "@multica/core/api";
import { WorkspacePackageSchema, type WorkspacePackage, type WorkspacePackageCounts } from "@multica/core/api/schemas";
import { defaultPackageOptions, packageSelection, useImportWorkspacePackage } from "@multica/core/workspace";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { PackageChecklist } from "./package-checklist";
import { useT } from "../i18n";

function ImportSelection({ wsId, pkg, counts, onComplete, onBusyChange }: {
  wsId: string; pkg: WorkspacePackage; counts: WorkspacePackageCounts; onComplete?: () => void; onBusyChange?: (busy: boolean) => void;
}) {
  const { t } = useT("workspace");
  const [selection, setSelection] = useState(() => packageSelection(pkg));
  const [replaceExisting, setReplaceExisting] = useState(false);
  const mutation = useImportWorkspacePackage(wsId);
  useEffect(() => {
    onBusyChange?.(mutation.isPending);
    return () => onBusyChange?.(false);
  }, [mutation.isPending, onBusyChange]);
  return <div className="space-y-4">
    <PackageChecklist wsId={wsId} counts={counts} pkg={pkg} selection={selection} onChange={setSelection} disabled={mutation.isPending || mutation.isSuccess} />
    <label className="flex items-start gap-2 text-caption text-muted-foreground">
      <Checkbox checked={replaceExisting} onCheckedChange={setReplaceExisting} disabled={mutation.isPending || mutation.isSuccess} />
      {t(($) => $.package.replace_existing)}
    </label>
    {mutation.isError && <p role="alert" className="text-caption text-destructive">{mutation.error.message}</p>}
    {mutation.isSuccess ? <p role="status" className="text-body">{t(($) => $.package.imported)}</p> :
      <Button disabled={mutation.isPending} aria-busy={mutation.isPending} onClick={() => mutation.mutate({ pkg, selection, replaceExisting }, { onSuccess: () => onComplete?.() })}>
        {mutation.isPending ? t(($) => $.package.importing) : t(($) => $.package.import_action)}
      </Button>}
  </div>;
}

export function WorkspaceImport({ wsId, defaultKit = false, onComplete, onBusyChange }: {
  wsId: string; defaultKit?: boolean; onComplete?: () => void; onBusyChange?: (busy: boolean) => void;
}) {
  const { t } = useT("workspace");
  const [uploaded, setUploaded] = useState<WorkspacePackage | null>(null);
  const [revision, setRevision] = useState(0);
  const [importBusy, setImportBusy] = useState(false);
  const handleBusyChange = useCallback((busy: boolean) => { setImportBusy(busy); onBusyChange?.(busy); }, [onBusyChange]);
  const defaults = useQuery({ ...defaultPackageOptions(wsId), enabled: defaultKit });
  const inspect = useMutation({
    mutationFn: async (file: File) => {
      const raw: unknown = JSON.parse(await file.text());
      const pkg = parseWithFallback(raw, WorkspacePackageSchema, null as WorkspacePackage | null, { endpoint: "workspace package file" });
      if (!pkg) throw new Error(t(($) => $.package.invalid));
      // Keep the checklist available so unsupported resources can be deselected.
      setUploaded(pkg);
      setRevision((n) => n + 1);
      const inventory = await api.inspectWorkspacePackage(wsId, pkg);
      if (!inventory) throw new Error(t(($) => $.package.failed));
      return pkg;
    },
  });
  const pkg = defaultKit ? defaults.data : uploaded;
  const counts = pkg ? {
    projects: pkg.projects.length, issues: pkg.issues.length, agents: pkg.agents.length,
    skills: pkg.skills.length, mcps: pkg.mcps.length, plugins: pkg.plugins.length, system_prompt: pkg.system_prompt != null,
  } : null;
  return <div className="space-y-4">
    {!defaultKit && <label className="block space-y-2 text-body">
      <span>{t(($) => $.package.file)}</span>
      <Input type="file" accept=".json,application/json" disabled={inspect.isPending || importBusy} onChange={(event) => {
        const file = event.target.files?.[0];
        setUploaded(null);
        if (file) inspect.mutate(file);
      }} />
    </label>}
    {(inspect.isPending || (defaultKit && defaults.isPending)) && <p role="status">{t(($) => $.package.loading)}</p>}
    {(inspect.isError || (defaultKit && (defaults.isError || defaults.data === null))) && <p role="alert" className="text-caption text-destructive">{inspect.error?.message ?? t(($) => $.package.failed)}</p>}
    {pkg && counts && !inspect.isPending && <ImportSelection key={revision} wsId={wsId} pkg={pkg} counts={counts} onComplete={() => { inspect.reset(); onComplete?.(); }} onBusyChange={handleBusyChange} />}
  </div>;
}
