"use client";

import { Button } from "@multica/ui/components/ui/button";
import { WorkspaceImport } from "../../workspace/workspace-import";
import { useT } from "../../i18n";
import { StepHeading } from "../components/step-shell";

export function StepImport({ wsId, completed, onNext, onBusyChange }: {
  wsId: string; completed: boolean; onNext: () => void; onBusyChange: (busy: boolean) => void;
}) {
  const { t } = useT("workspace");
  return <div className="flex flex-col gap-6">
    <StepHeading title={t(($) => $.package.startup_title)} />
    {completed ? <Button onClick={onNext}>{t(($) => $.package.continue)}</Button> :
      <WorkspaceImport wsId={wsId} defaultKit onComplete={onNext} onBusyChange={onBusyChange} />}
  </div>;
}
