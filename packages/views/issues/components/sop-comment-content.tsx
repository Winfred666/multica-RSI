"use client";

import { useState } from "react";
import { useCreateComment } from "@multica/core/issues/mutations";
import { parseSOPComment, sopAnswerContent, sopParameters, sopSelection, type SOPQuestion } from "@multica/core/issues/sop-question";
import type { TimelineEntry } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Code } from "@multica/ui/icons";
import { DeferredTooltip } from "../../common/deferred-tooltip";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { ReadonlyContent } from "../../editor";
import { useT } from "../../i18n";

export function SOPRawToggle({ entry, pressed, onClick, className }: {
  entry: TimelineEntry;
  pressed: boolean;
  onClick: () => void;
  className?: string;
}) {
  const { t } = useT("editor");
  if (!parseSOPComment(entry)) return null;
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
  if (!question) return <ReadonlyContent content={entry.content ?? ""} attachments={entry.attachments} />;
  return question.kind === "remote"
    ? <pre className={`overflow-x-auto whitespace-pre-wrap break-all font-mono text-caption${raw ? "" : " rounded-md bg-muted/50 p-3"}`}>{raw ? entry.content : [question.sop, ...question.args].join(" ")}</pre>
    : <SOPContent key={entry.content} issueId={issueId} entry={entry} question={question} currentUserId={currentUserId} raw={raw} />;
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
