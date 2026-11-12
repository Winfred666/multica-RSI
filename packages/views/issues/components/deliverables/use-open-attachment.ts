"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import type { Attachment } from "@multica/core/types";
import {
  useAttachmentPreview,
  useDownloadAttachment,
  usePreviewSequence,
} from "../../../editor";

/**
 * Open a file in the issue's viewer at its place in the page's sequence. A
 * file the sequence doesn't hold opens on its own, and one the viewer can't
 * show downloads. Render `modal` for the on-its-own case.
 */
export function useOpenAttachment() {
  const client = useQueryClient();
  const sequence = usePreviewSequence();
  const preview = useAttachmentPreview();
  const download = useDownloadAttachment();
  const { openAt } = sequence;
  const { tryOpen } = preview;
  const open = useCallback(
    (attachment: Attachment) => {
      if (!attachment.issue_id && !attachment.comment_id) {
        for (const key of ["attachment-content", "attachment-inline-resign", "attachment-inline-blob"]) {
          void client.invalidateQueries({ queryKey: [key, attachment.id] });
        }
      }
      if (openAt(attachment.id)) return;
      if (tryOpen({ kind: "full", attachment })) return;
      download(attachment.id);
    },
    [client, openAt, tryOpen, download],
  );
  return { open, modal: preview.modal };
}
