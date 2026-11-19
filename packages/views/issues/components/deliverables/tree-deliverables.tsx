"use client";

import { cn } from "@multica/ui/lib/utils";
import { useIssueSurfaceSelection } from "../../surface/selection-context";
import type { DeliverableFile } from "@multica/core/attachments/deliverables";
import { useT } from "../../../i18n";
import { useOpenAttachment } from "./use-open-attachment";

export function TreeDeliverables({ files }: { files: ReadonlyArray<DeliverableFile> }) {
  const { t } = useT("issues");
  const { open, modal } = useOpenAttachment();
  const selection = useIssueSurfaceSelection();
  return (
    <div className="space-y-2 p-2" aria-label={t(($) => $.deliverables.section_title)}>
      {[true, false].flatMap((independent) =>
        files.filter((file) => !!file.independent === independent).map(({ key, latest, versions }) => (
          <button key={key} type="button" data-tree-artifact={latest.id} data-attachment-ids={versions.map((file) => file.id).join(",")} aria-pressed={selection.selectedIds.has(latest.id)} onClick={() => open(latest)} title={latest.filename}
            className={cn("flex w-full items-center rounded-md border border-surface-border bg-surface p-2 text-caption focus-visible:outline-2 focus-visible:outline-ring", selection.selectedIds.has(latest.id) && "ring-1 ring-brand ring-offset-1 ring-offset-background")}>
            <span className="min-w-0 truncate">{latest.filename}</span>
          </button>
        )),
      )}
      {modal}
    </div>
  );
}
