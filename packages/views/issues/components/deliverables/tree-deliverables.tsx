"use client";

import type { DeliverableFile } from "@multica/core/attachments/deliverables";
import { useT } from "../../../i18n";
import { getPreviewKind } from "../../../editor/utils/preview";
import { DeliverableThumbnail } from "./deliverable-thumbnail";
import { useOpenAttachment } from "./use-open-attachment";

export function TreeDeliverables({ files }: { files: ReadonlyArray<DeliverableFile> }) {
  const { t } = useT("issues");
  const { open, modal } = useOpenAttachment();
  const html = files.filter(({ latest }) => getPreviewKind(latest.content_type, latest.filename) === "html");
  const images = files.filter(({ latest }) => getPreviewKind(latest.content_type, latest.filename) === "image");
  const others = files.filter((file) => !html.includes(file) && !images.includes(file));
  return (
    <div className="space-y-2 p-2" aria-label={t(($) => $.deliverables.section_title)}>
      {[...html, ...images].map(({ key, latest }) => (
        <button key={key} type="button" aria-label={latest.filename} onClick={() => open(latest)}
          className="block aspect-[3/2] w-full overflow-hidden rounded-md border border-surface-border bg-surface focus-visible:outline-2 focus-visible:outline-ring">
          <DeliverableThumbnail attachment={latest} className="size-full" />
        </button>
      ))}
      {others.length > 0 && <div className="-space-y-3">
        {others.map(({ key, latest }) => (
          <button key={key} type="button" onClick={() => open(latest)} title={latest.filename}
            className="relative flex w-full items-center gap-2 rounded-md border border-surface-border bg-surface p-2 text-caption shadow-sm hover:z-10 focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-ring">
            <DeliverableThumbnail attachment={latest} showTypeLabel={false} className="size-6 shrink-0 rounded-md" />
            <span className="min-w-0 truncate">{latest.filename}</span>
          </button>
        ))}
      </div>}
      {modal}
    </div>
  );
}
