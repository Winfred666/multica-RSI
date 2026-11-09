"use client";

import { useCallback, useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api, parseWithFallback } from "@multica/core/api";
import { useFeatureEnabled } from "@multica/core/config";
import { PLUGINS_V1_FLAG } from "@multica/core/feature-flags";
import { WorkspacePackageSchema, type WorkspacePackage, type WorkspacePackageCounts } from "@multica/core/api/schemas";
import { defaultPackageOptions, packageSelection, useImportWorkspacePackage } from "@multica/core/workspace";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { PackageChecklist } from "./package-checklist";
import { useT } from "../i18n";

function ImportSelection({ wsId, pkg, counts, defaultKit, onComplete, onBusyChange }: {
  wsId: string; pkg: WorkspacePackage; counts: WorkspacePackageCounts; defaultKit: boolean; onComplete?: () => void; onBusyChange?: (busy: boolean) => void;
}) {
  const { t } = useT("workspace");
  const [selection, setSelection] = useState(() => packageSelection(pkg));
  const pluginsEnabled = useFeatureEnabled(PLUGINS_V1_FLAG, false);
  const [activatePlugins, setActivatePlugins] = useState(defaultKit);
  const currentToken = api.getToken();
  const currentPat = currentToken?.startsWith("mul_") ? currentToken : undefined;
  const mutation = useImportWorkspacePackage(wsId);
  const emptySelection = Object.values(selection).every((value) => Array.isArray(value) ? value.length === 0 : !value);
  const missingStartupResources = !selection.system_prompt || selection.skills.length < pkg.skills.length || selection.plugins.length < pkg.plugins.length;
  useEffect(() => {
    onBusyChange?.(mutation.isPending);
    return () => onBusyChange?.(false);
  }, [mutation.isPending, onBusyChange]);
  return <div className="space-y-4">
    {defaultKit && <Button variant="ghost" size="sm" disabled={mutation.isPending || mutation.isSuccess || emptySelection} onClick={() => setSelection(packageSelection())}>
      {t(($) => $.package.unselect_all)}
    </Button>}
    <PackageChecklist wsId={wsId} counts={counts} pkg={pkg} selection={selection} onChange={setSelection} disabled={mutation.isPending || mutation.isSuccess} />
    {defaultKit && missingStartupResources && <p role="alert" className="text-caption text-destructive">{t(($) => $.package.startup_warning)}</p>}
    {pluginsEnabled && currentPat && <div className="space-y-2">
      <label className="flex items-center gap-2 text-body">
        <Switch checked={activatePlugins} onCheckedChange={setActivatePlugins} disabled={mutation.isPending || mutation.isSuccess} />
        {t(($) => $.package.activate_plugins)}
      </label>
      <p className="text-caption text-muted-foreground">{t(($) => $.package.plugin_execution_hint)}</p>
    </div>}
    {mutation.isError && <p role="alert" className="text-caption text-destructive">{mutation.error.message}</p>}
    {mutation.isSuccess ? <p role="status" className="text-body">{t(($) => $.package.imported)}</p> :
      <Button disabled={mutation.isPending} aria-busy={mutation.isPending} onClick={() => mutation.mutate({ pkg, selection, pluginExecutionToken: pluginsEnabled && activatePlugins ? currentPat : undefined }, { onSuccess: () => onComplete?.() })}>
        {mutation.isPending ? t(($) => $.package.importing) : t(($) => $.package.import_action)}
      </Button>}
  </div>;
}

export function WorkspaceImport({ wsId, defaultKit = false, onComplete, onBusyChange }: {
  wsId: string; defaultKit?: boolean; onComplete?: () => void; onBusyChange?: (busy: boolean) => void;
}) {
  const { t } = useT("workspace");
  const [uploaded, setUploaded] = useState<WorkspacePackage | null>(null);
  const [importBusy, setImportBusy] = useState(false);
  const handleBusyChange = useCallback((busy: boolean) => { setImportBusy(busy); onBusyChange?.(busy); }, [onBusyChange]);
  const defaults = useQuery(defaultPackageOptions(wsId));
  const inspect = useMutation({
    mutationFn: async (file: File) => {
      const raw: unknown = JSON.parse(await file.text());
      const pkg = parseWithFallback(raw, WorkspacePackageSchema, null as WorkspacePackage | null, { endpoint: "workspace package file" });
      if (!pkg) throw new Error(t(($) => $.package.invalid));
      // Keep the checklist available so unsupported resources can be deselected.
      setUploaded(pkg);
      const inventory = await api.inspectWorkspacePackage(wsId, pkg);
      if (!inventory) throw new Error(t(($) => $.package.failed));
      return pkg;
    },
  });
  const pkg = uploaded ?? defaults.data;
  const counts = pkg ? {
    projects: pkg.projects.length, issues: pkg.issues.length, agents: pkg.agents.length,
    skills: pkg.skills.length, mcps: pkg.mcps.length, plugins: pkg.plugins.length, system_prompt: pkg.system_prompt != null,
  } : null;
  return <div className="space-y-4">
    {!uploaded && <p className="text-caption text-muted-foreground">{t(($) => $.package.default_source)}</p>}
    <label className="block space-y-2 text-body">
      <span>{t(($) => $.package.file)}</span>
      <Input type="file" accept=".json,application/json" disabled={inspect.isPending || importBusy} onChange={(event) => {
        const file = event.target.files?.[0];
        setUploaded(null);
        if (file) inspect.mutate(file);
      }} />
    </label>
    {(inspect.isPending || (!uploaded && defaults.isPending)) && <p role="status">{t(($) => $.package.loading)}</p>}
    {(inspect.isError || (!uploaded && (defaults.isError || defaults.data === null))) && <p role="alert" className="text-caption text-destructive">{inspect.error?.message ?? t(($) => $.package.failed)}</p>}
    {pkg && counts && !inspect.isPending && <ImportSelection key={uploaded ? "uploaded" : "default"} wsId={wsId} pkg={pkg} counts={counts} defaultKit={defaultKit} onComplete={() => { inspect.reset(); onComplete?.(); }} onBusyChange={handleBusyChange} />}
  </div>;
}
