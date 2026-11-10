"use client";

import { useState, type Dispatch, type SetStateAction } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { packageInventoryOptions, packageItemsOptions } from "@multica/core/workspace";
import type { WorkspacePackage, WorkspacePackageCategory, WorkspacePackageCounts, WorkspacePackageItem, WorkspacePackageSelection } from "@multica/core/api/schemas";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@multica/ui/components/ui/collapsible";
import { ChevronRight } from "@multica/ui/icons";
import { useT } from "../i18n";

function CheckRow({ label, checked, indeterminate, disabled, replacing, onChange, children }: {
  label: string; checked: boolean; indeterminate?: boolean; disabled?: boolean; replacing?: boolean;
  onChange: (checked: boolean) => void; children?: React.ReactNode;
}) {
  const { t } = useT("workspace");
  return <div className="flex min-w-0 items-center gap-2 py-2">
    {children}
    <label className="flex min-w-0 flex-1 items-center gap-2 text-body">
      <Checkbox checked={checked} indeterminate={indeterminate} disabled={disabled} onCheckedChange={onChange} />
      <span className="break-words">{label}</span>
      {replacing && <span className="shrink-0 rounded-sm bg-muted px-1.5 text-caption text-muted-foreground">{t(($) => $.package.replaces_existing)}</span>}
    </label>
  </div>;
}

function ItemGroup({ label, items, selected, onChange, disabled, replacing }: {
  label: string; items: WorkspacePackageItem[]; selected: string[];
  onChange: (ids: string[]) => void; disabled: boolean; replacing: Set<string>;
}) {
  const count = items.filter((item) => selected.includes(item.id)).length;
  return <Collapsible>
    <CheckRow label={`${label} (${items.length})`} checked={items.length > 0 && count === items.length}
      indeterminate={count > 0 && count < items.length} disabled={disabled || !items.length}
      onChange={(checked) => onChange(checked ? [...selected.filter((id) => !items.some((item) => item.id === id)), ...items.map((item) => item.id)] : selected.filter((id) => !items.some((item) => item.id === id)))}>
      <CollapsibleTrigger aria-label={label} render={<Button variant="ghost" size="icon-xs" />}>
        <ChevronRight className="group-aria-expanded/button:rotate-90" />
      </CollapsibleTrigger>
    </CheckRow>
    <CollapsibleContent className="ml-7 border-l pl-3">{items.map((item) => <CheckRow key={item.id}
      label={item.name} checked={selected.includes(item.id)} disabled={disabled} replacing={replacing.has(item.id)}
      onChange={(checked) => onChange(checked ? [...selected, item.id] : selected.filter((id) => id !== item.id))} />)}</CollapsibleContent>
  </Collapsible>;
}

function Category({ category, wsId, count, pkg, selected, onChange, disabled }: {
  category: Exclude<WorkspacePackageCategory, "issues">; wsId: string; count: number; pkg?: WorkspacePackage;
  selected: string[]; onChange: (ids: string[]) => void; disabled: boolean;
}) {
  const { t } = useT("workspace");
  const qc = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const [selecting, setSelecting] = useState(false);
  const [error, setError] = useState(false);
  const query = useQuery({ ...packageItemsOptions(wsId, category), enabled: expanded && (!pkg || (category !== "agents" && category !== "projects")) });
  const packageItems = pkg && (category === "projects" ? pkg.projects.map((item) => ({ id: item.id, name: item.title })) : pkg[category]);
  const items: WorkspacePackageItem[] = packageItems ?? query.data ?? [];
  const replacing = new Set(pkg && category !== "projects" ? items.filter((item) => query.data?.some((existing) => category === "plugins" ? !!item.key && existing.key === item.key : existing.name === item.name)).map((item) => item.id) : []);
  const label = t(($) => $.package[category]);
  const toggleAll = async (checked: boolean) => {
    if (!checked) { onChange([]); return; }
    setSelecting(true);
    setError(false);
    try { onChange((packageItems ?? await qc.fetchQuery(packageItemsOptions(wsId, category))).map((item) => item.id)); }
    catch { setError(true); }
    finally { setSelecting(false); }
  };
  return <Collapsible open={expanded} onOpenChange={setExpanded}>
    <CheckRow label={`${label} (${count})`} checked={count > 0 && selected.length === count}
      indeterminate={selected.length > 0 && selected.length < count} disabled={disabled || selecting || count === 0} onChange={(checked) => void toggleAll(checked)}>
      <CollapsibleTrigger aria-label={label} render={<Button variant="ghost" size="icon-xs" />}>
        <ChevronRight className="group-aria-expanded/button:rotate-90" />
      </CollapsibleTrigger>
    </CheckRow>
    {category === "agents" && <p className="ml-7 text-caption text-muted-foreground">{t(($) => $.package.agents_hint)}</p>}
    {(error || query.isError) && <p role="alert" className="text-caption text-destructive">{t(($) => $.package.failed)}</p>}
    <CollapsibleContent className="ml-7 max-h-64 overflow-y-auto border-l pl-3">
      {!pkg && query.isPending ? <p role="status" className="py-2 text-caption">{t(($) => $.package.loading)}</p> : category === "skills" ? <>
        <ItemGroup label={t(($) => $.package.system_skills)} items={items.filter((item) => item.is_system_skill === true)} selected={selected} onChange={onChange} disabled={disabled} replacing={replacing} />
        <ItemGroup label={t(($) => $.package.normal_skills)} items={items.filter((item) => item.is_system_skill !== true)} selected={selected} onChange={onChange} disabled={disabled} replacing={replacing} />
      </> : items.map((item) => <CheckRow key={item.id} label={item.name} checked={selected.includes(item.id)} disabled={disabled} replacing={replacing.has(item.id)}
        onChange={(checked) => onChange(checked ? [...selected, item.id] : selected.filter((id) => id !== item.id))} />)}
    </CollapsibleContent>
  </Collapsible>;
}

export function PackageChecklist({ wsId, counts, pkg, selection, onChange, disabled = false }: {
  wsId: string; counts: WorkspacePackageCounts; pkg?: WorkspacePackage;
  selection: WorkspacePackageSelection; onChange: Dispatch<SetStateAction<WorkspacePackageSelection>>; disabled?: boolean;
}) {
  const { t } = useT("workspace");
  const inventory = useQuery({ ...packageInventoryOptions(wsId), enabled: !!pkg && counts.system_prompt });
  const issues = useQuery({ ...packageItemsOptions(wsId, "issues"), enabled: !pkg, refetchOnMount: "always" });
  const issueItems = pkg?.issues ?? issues.data;
  return <fieldset disabled={disabled} className="min-w-0 space-y-1">
    <Category category="projects" wsId={wsId} count={counts.projects} pkg={pkg} selected={selection.projects}
      onChange={(ids) => onChange((current) => ({ ...current, projects: ids }))} disabled={disabled} />
    <div className="pl-7">
      <CheckRow label={`${t(($) => $.package.issues)} (${counts.issues})`} checked={counts.issues > 0 && selection.issues.length === counts.issues}
        disabled={disabled || counts.issues === 0 || (!pkg && (issues.isFetching || issues.isError || !issueItems))}
        onChange={(checked) => onChange((current) => ({ ...current, issues: checked ? (issueItems ?? []).map((item) => item.id) : [] }))} />
      {!pkg && issues.isFetching && <p role="status" className="text-caption">{t(($) => $.package.loading)}</p>}
      {!pkg && issues.isError && <p role="alert" className="text-caption text-destructive">{t(($) => $.package.failed)}</p>}
    </div>
    {(["agents", "skills", "mcps", "plugins"] as const).map((category) => <Category key={category} category={category} wsId={wsId} count={counts[category]} pkg={pkg}
      selected={selection[category]} onChange={(ids) => onChange((current) => ({ ...current, [category]: ids }))} disabled={disabled} />)}
    <div className="pl-7"><CheckRow label={t(($) => $.package.system_prompt)} checked={selection.system_prompt} replacing={!!pkg && inventory.data?.counts.system_prompt}
      disabled={disabled || !counts.system_prompt} onChange={(checked) => onChange((current) => ({ ...current, system_prompt: checked }))} /></div>
  </fieldset>;
}
