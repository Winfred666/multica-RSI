"use client";

import { useCallback, useLayoutEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { sourceContextPreviewOptions } from "@multica/core/issues/queries";
import { useWSEvent, useWSReconnect } from "@multica/core/realtime";
import { useCreateComment } from "@multica/core/issues/mutations";
import { parseSOPComment, sopQuestionLink, sopAnswerContent, sopParameters, sopSelection, type SOPQuestion } from "@multica/core/issues/sop-question";
import type { CommentCreatedPayload, CommentDeletedPayload, SourceContextCommentSnapshot, TimelineEntry } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Code } from "@multica/ui/icons";
import { DeferredTooltip } from "../../common/deferred-tooltip";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { ReadonlyContent } from "../../editor";
import { useT } from "../../i18n";
import { SourceContextCommentList } from "./source-context-comment-list";

export function SOPRawToggle({ entry, pressed, onClick, className }: {
  entry: TimelineEntry;
  pressed: boolean;
  onClick: () => void;
  className?: string;
}) {
  const { t } = useT("editor");
  if (!parseSOPComment(entry) && !forwardLink(entry)) return null;
  const label = t(($) => $.attachment.view_source);
  return <DeferredTooltip content={label} trigger={
    <Button type="button" variant={pressed ? "secondary" : "ghost"} size="icon-sm" className={className}
      aria-label={label} aria-pressed={pressed} onClick={onClick}>
      <Code className="size-4" aria-hidden />
    </Button>
  } />;
}

export function SOPCommentContent({ issueId, entry, currentUserId, raw = false }: {
  issueId: string;
  entry: TimelineEntry;
  currentUserId?: string;
  raw?: boolean;
}) {
  const question = parseSOPComment(entry);
  if (!question) {
    const link = forwardLink(entry);
    if (link) return raw
      ? <pre className="overflow-x-auto whitespace-pre-wrap break-all font-mono text-caption">{entry.content}</pre>
      : <SOPSourceThread entry={entry} link={link} currentUserId={currentUserId} />;
    return <ReadonlyContent content={entry.content ?? ""} attachments={entry.attachments} />;
  }
  return question.kind === "remote"
    ? <pre className={`overflow-x-auto whitespace-pre-wrap break-all font-mono text-caption${raw ? "" : " rounded-md bg-muted/50 p-3"}`}>{raw ? entry.content : [question.sop, ...question.args].join(" ")}</pre>
    : <SOPContent key={entry.content} issueId={issueId} entry={entry} question={question} currentUserId={currentUserId} raw={raw} />;
}

function forwardLink(entry: TimelineEntry) {
  return entry.type === "comment" && !entry.deleted_at ? sopQuestionLink(entry.content ?? "") : null;
}

function SourcePermalink({ href, title }: { href: string; title: string }) {
  return <a href={href} className="block truncate text-caption text-muted-foreground underline underline-offset-2" title={title}>{title}</a>;
}

function sourceEntry(comment: SourceContextCommentSnapshot): TimelineEntry {
  return { id: comment.id, type: "comment", actor_type: comment.author.type, actor_id: comment.author.id,
    content: comment.content, created_at: comment.created_at, parent_id: comment.parent_id,
    deleted_at: comment.deleted ? comment.updated_at : undefined };
}

function SOPSourceThread({ entry, link, currentUserId }: {
  entry: TimelineEntry;
  link: NonNullable<ReturnType<typeof sopQuestionLink>>;
  currentUserId?: string;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const preview = useQuery(sourceContextPreviewOptions(wsId, link.commentId));
  const viewport = useRef<HTMLDivElement>(null);
  const sourceId = preview.data?.source_issue.id;
  const refresh = useCallback(() => { void preview.refetch(); }, [preview.refetch]);
  const onComment = useCallback((payload: unknown) => {
    if ((payload as CommentCreatedPayload).comment?.issue_id === sourceId) refresh();
  }, [refresh, sourceId]);
  const onDelete = useCallback((payload: unknown) => {
    if ((payload as CommentDeletedPayload).issue_id === sourceId) refresh();
  }, [refresh, sourceId]);
  useWSEvent("comment:created", onComment);
  useWSEvent("comment:updated", onComment);
  useWSEvent("comment:deleted", onDelete);
  useWSReconnect(refresh);
  const anchor = preview.data?.comment_thread.find((comment) => comment.id === link.commentId);
  const question = anchor && parseSOPComment(sourceEntry(anchor));
  useLayoutEffect(() => {
    const window = viewport.current;
    const card = window?.querySelector<HTMLElement>(`[data-source-comment-id="${link.commentId}"]`);
    if (!window || !card) return;
    let initial = true;
    const resize = () => {
      window.style.height = `${card.getBoundingClientRect().height + 24}px`;
      if (initial) { window.scrollTop = window.scrollHeight; initial = false; }
    };
    resize();
    const observer = new ResizeObserver(resize);
    observer.observe(card);
    return () => observer.disconnect();
  }, [link.commentId, !!question]);
  if (preview.isError || (preview.data && question?.kind !== "render")) return <div className="space-y-2">
    <ReadonlyContent content={entry.content ?? ""} attachments={entry.attachments} />
    {preview.isError && <p role="alert" className="text-caption text-muted-foreground">{t(($) => $.sop.context_unavailable)}</p>}
  </div>;
  return <div className="space-y-3">
    {link.prose && <ReadonlyContent content={link.prose} attachments={entry.attachments} />}
    <section className="min-w-0 rounded-md border" aria-label={t(($) => $.sop.source_thread)}>
      <div className="border-b px-3 py-2"><SourcePermalink href={link.href} title={preview.data?.source_issue.title || link.title || link.href} /></div>
      {preview.data ? <div ref={viewport} tabIndex={0} className="overflow-y-auto overscroll-contain p-3" data-sop-source-window>
        <SourceContextCommentList comments={preview.data.comment_thread} anchorCommentId={link.commentId}
          renderContent={(comment) => comment.id === link.commentId
            ? <SOPCommentContent issueId={preview.data.source_issue.id} entry={sourceEntry(comment)} currentUserId={currentUserId} />
            : <ReadonlyContent content={comment.content} />} />
      </div> : <p role="status" className="p-3 text-caption text-muted-foreground">{t(($) => $.sop.loading_context)}</p>}
    </section>
  </div>;
}

function SOPContent({ issueId, entry, question, currentUserId, raw }: {
  issueId: string;
  entry: TimelineEntry;
  question: SOPQuestion;
  currentUserId?: string;
  raw: boolean;
}) {
  const { t } = useT("issues");
  const submit = useCreateComment(issueId);
  const [selected, setSelected] = useState<number | null>(null);
  const [values, setValues] = useState<Record<string, string>>({});
  const answered = question.args !== undefined;
  const answer = sopSelection(question);
  const choiceIndex = answer?.index ?? selected;
  const option = choiceIndex === null ? undefined : question.next_step[choiceIndex];
  const fields = option ? sopParameters(option) : [];
  const locked = answered || submit.isPending || submit.isSuccess || !currentUserId;
  if (raw) return <pre className="overflow-x-auto whitespace-pre-wrap break-all font-mono text-caption">{entry.content}</pre>;
  return (
    <section className="min-w-0 space-y-3">
      <div className="flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
        <span className="break-all font-medium text-foreground">{question.sop}</span>
        <span>{question.status}</span>
        {(answered || question.error || question.next_step.length > 0) && <Badge variant={question.error ? "destructive" : "secondary"}>
          {answered ? t(($) => $.sop.answered) : question.error ? t(($) => $.sop.failed) : t(($) => $.sop.waiting)}
        </Badge>}
      </div>
      <ReadonlyContent content={question.instruction} attachments={entry.attachments} />
      {question.error && <p role="alert" className="text-caption text-destructive">{question.error.message}</p>}
      {question.next_step.length > 0 && <form className="space-y-3" aria-busy={submit.isPending} onSubmit={(event) => {
        event.preventDefault();
        if (!locked && option && fields.every((field) => values[field]?.trim())) {
          submit.mutate({ content: sopAnswerContent(option, values, entry.id), parentId: entry.id });
        }
      }}>
        <fieldset disabled={locked} className="space-y-2">
          <legend className="sr-only">{question.instruction}</legend>
          {question.next_step.map((choice, index) => <label key={choice.command} className="flex cursor-pointer items-start gap-2 text-body">
            <input type="radio" className="mt-1 accent-primary" name={`sop-option-${entry.id}`} value={index}
              checked={choiceIndex === index} onChange={() => { setSelected(index); setValues({}); }} />
            <span>{choice.instruction}</span>
          </label>)}
          {fields.map((name) => <label key={name} className="block space-y-1 text-caption">
            <span>{name}</span>
            <Input required name={name} value={answer?.values[name] ?? values[name] ?? ""}
              onChange={(event) => setValues({ ...values, [name]: event.target.value })} />
          </label>)}
        </fieldset>
        {!answered && <Button type="submit" size="sm" disabled={locked || !option || fields.some((field) => !values[field]?.trim())}>
          {submit.isPending ? t(($) => $.sop.submitting) : t(($) => $.sop.submit)}
        </Button>}
        {submit.isError && <p role="alert" className="text-caption text-destructive">{submit.error.message}</p>}
        {submit.isSuccess && <p role="status" className="text-caption text-muted-foreground">{t(($) => $.sop.submitted)}</p>}
      </form>}
    </section>
  );
}
