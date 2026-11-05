"use client";

import { useState } from "react";
import type { Attachment } from "@multica/core/types";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { cn } from "@multica/ui/lib/utils";
import { useResignedInlineMedia } from "../../../editor/hooks/use-inline-media-url";
import { fileTypeLabel, getPreviewKind } from "../../../editor/utils/preview";
import { fileIcon } from "../../../editor/utils/file-icon";
import { CodeBlockIframe } from "../../../editor/code-block-iframe";
import { useAttachmentHtmlText } from "../../../editor/hooks/use-attachment-html-text";

/**
 * A file's face in a grid or strip: an HTML or image preview, otherwise
 * its type icon and extension. Fills its box — the caller sizes it.
 */
export function DeliverableThumbnail({
  attachment,
  showTypeLabel = true,
  className,
}: {
  attachment: Attachment;
  /** The extension under the icon; off where the tile is too small for it. */
  showTypeLabel?: boolean;
  className?: string;
}) {
  const kind = getPreviewKind(attachment.content_type, attachment.filename);
  if (kind === "html") {
    return <HtmlThumbnail attachment={attachment} className={className} />;
  }
  if (kind === "image") {
    return <ImageThumbnail key={attachment.url} attachment={attachment} className={className} />;
  }
  return (
    <FileFace
      attachment={attachment}
      showTypeLabel={showTypeLabel}
      className={className}
    />
  );
}

function HtmlThumbnail({ attachment, className }: { attachment: Attachment; className?: string }) {
  const { data } = useAttachmentHtmlText(attachment.id);
  return data ? (
    <div inert className={cn("pointer-events-none", className)}>
      <CodeBlockIframe html={data.text} title={attachment.filename} heightClassName="h-full" className="rounded-none border-0" />
    </div>
  ) : <FileFace attachment={attachment} showTypeLabel className={className} />;
}

function FileFace({
  attachment,
  showTypeLabel,
  className,
}: {
  attachment: Attachment;
  showTypeLabel: boolean;
  className?: string;
}) {
  const Icon = fileIcon(attachment.content_type, attachment.filename);
  const label = fileTypeLabel(attachment.filename);
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-1.5 bg-muted text-muted-foreground",
        className,
      )}
    >
      <Icon className="size-5" />
      {showTypeLabel && label && (
        <span className="text-micro font-medium tracking-wide">{label}</span>
      )}
    </div>
  );
}

// Loads the original — there is no thumbnail endpoint yet — so callers keep
// these few (the sidebar shows three) or lazy (the overview's grid).
function ImageThumbnail({
  attachment,
  className,
}: {
  attachment: Attachment;
  className?: string;
}) {
  const picked =
    attachment.download_url || attachment.markdown_url || attachment.url;
  const { url, pending } = useResignedInlineMedia(
    attachment.id,
    resolvePublicFileUrl(picked) ?? picked,
    true,
  );
  const [failedUrl, setFailedUrl] = useState<string | null>(null);

  if (pending || !url || failedUrl === url) {
    return (
      <FileFace attachment={attachment} showTypeLabel={false} className={className} />
    );
  }
  return (
    <img
      src={url}
      alt=""
      loading="lazy"
      decoding="async"
      draggable={false}
      onError={() => setFailedUrl(url)}
      className={cn("bg-muted object-cover", className)}
    />
  );
}
