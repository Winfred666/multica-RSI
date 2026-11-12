"use client";

import { useQuery } from "@tanstack/react-query";
import { linkedDeliverables } from "@multica/core/attachments/deliverables";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Attachment } from "@multica/core/types";
import { workspaceDeliverablesOptions } from "@multica/core/workspace/queries";
import { Link } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../../i18n";
import { useOpenAttachment } from "./use-open-attachment";

export function IssueLinkedDeliverables({ value }: { value: unknown }) {
  const { data = [] } = useQuery(workspaceDeliverablesOptions(useWorkspaceId()));
  return <LinkedDeliverables files={linkedDeliverables(value, data)} />;
}

export function LinkedDeliverables({ files }: { files: readonly Attachment[] }) {
  const { t } = useT("issues");
  const { open, modal } = useOpenAttachment();
  if (!files.length) return null;
  return <section className="min-w-0 space-y-1 p-2" aria-label={t(($) => $.deliverables.linked_title)}>
    <h3 className="px-2 text-caption font-medium text-muted-foreground">{t(($) => $.deliverables.linked_title)}</h3>
    {files.map((file) => <Button key={file.id} variant="ghost" size="sm" className="w-full justify-start" onClick={() => open(file)} data-deliverable-link={file.id}>
      <Link className="size-4 shrink-0" /><span className="truncate">{file.filename}</span>
    </Button>)}
    {modal}
  </section>;
}
