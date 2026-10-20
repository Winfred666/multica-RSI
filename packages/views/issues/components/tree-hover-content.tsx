"use client";

import { useMemo } from "react";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useTaskMessages } from "@multica/core/chat/queries";
import { issueTasksOptions } from "@multica/core/issues/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import type { AgentTask, Issue, TimelineEntry } from "@multica/core/types";
import type { IssueTreeCollaborator } from "@multica/core/issues/surface/tree";
import { Button } from "@multica/ui/components/ui/button";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@multica/ui/components/ui/hover-card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { ActorAvatar } from "../../common/actor-avatar";
import { buildTimeline } from "../../common/task-transcript/build-timeline";
import { RichContent } from "../../rich-content";
import { useT, useTimeAgo } from "../../i18n";
import { AttachmentList } from "./comment-card";

export function TreeIssueHoverContent({ issue, report, timeline }: {
  issue: Issue;
  report?: TimelineEntry;
  timeline: UseQueryResult<TimelineEntry[]>;
}) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const timeAgo = useTimeAgo();

  return (
    <div className="space-y-3 text-left">
      <div>
        <p className="font-mono text-micro text-muted-foreground" translate="no">{issue.identifier}</p>
        <h3 className="break-words text-body font-semibold">{issue.title}</h3>
      </div>
      <Tabs defaultValue="report">
        <TabsList aria-label={issue.identifier}>
          <TabsTrigger value="report">{t(($) => $.tree.report)}</TabsTrigger>
          <TabsTrigger value="description">{t(($) => $.tree.description)}</TabsTrigger>
        </TabsList>
        <TabsContent value="report" className="max-h-[min(28rem,55vh)] overflow-auto overscroll-contain break-words">
          {report && (report.content || report.attachments?.length) ? (
            <>
              <p className="mb-2 text-caption text-muted-foreground">{report.actor_name || getActorName("agent", report.actor_id)} · {timeAgo(report.created_at)}</p>
              {report.content && <RichContent content={report.content} />}
              <AttachmentList attachments={report.attachments} content={report.content} />
            </>
          ) : !timeline.isError && <p className="text-caption text-muted-foreground">{timeline.isPending ? t(($) => $.tree.loading_report) : t(($) => $.tree.no_report)}</p>}
          {timeline.isError && <Button variant="outline" size="sm" onClick={() => void timeline.refetch()}>{t(($) => $.status_catalog_error.retry)}</Button>}
        </TabsContent>
        <TabsContent value="description" className="max-h-[min(28rem,55vh)] overflow-auto overscroll-contain break-words">
          {issue.description ? <RichContent content={issue.description} /> : <p className="text-caption text-muted-foreground">{t(($) => $.tree.no_description)}</p>}
        </TabsContent>
      </Tabs>
    </div>
  );
}

export function TreeCollaboratorAvatar({ collaborator, issue, tasks }: {
  collaborator: IssueTreeCollaborator;
  issue: Issue;
  tasks: AgentTask[];
}) {
  const { getActorName } = useActorName();
  const latest = collaborator.messages[0];
  const name = latest?.actor_name || getActorName(collaborator.actorType, collaborator.actorId);
  return (
    <HoverCard>
      <HoverCardTrigger render={<span />} tabIndex={0} aria-label={name} className="inline-flex rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <ActorAvatar actorType={collaborator.actorType} actorId={collaborator.actorId} name={name} avatarUrl={latest?.actor_avatar_url} size="sm" profileLink={false} />
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-120 max-w-[calc(100vw-1rem)] p-3">
        <TreeCollaboratorHoverContent collaborator={collaborator} issue={issue} tasks={tasks} />
      </HoverCardContent>
    </HoverCard>
  );
}

function TreeCollaboratorHoverContent({ collaborator, issue, tasks }: {
  collaborator: IssueTreeCollaborator;
  issue: Issue;
  tasks: AgentTask[];
}) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const timeAgo = useTimeAgo();
  const isAgent = collaborator.actorType === "agent";
  const history = useQuery({ ...issueTasksOptions(issue.id), enabled: isAgent });
  const candidates = [...new Map([...(history.data ?? []), ...tasks].map((task) => [task.id, task])).values()]
    .filter((task) => isAgent && task.agent_id === collaborator.actorId && task.issue_id === issue.id)
    .sort((a, b) => Number(b.status === "running") - Number(a.status === "running") || Date.parse(b.created_at) - Date.parse(a.created_at));
  const task = candidates[0];
  const messages = useTaskMessages(task?.id ?? "", task?.status === "running");
  const entries = useMemo(() => buildTimeline(messages.data ?? [])
    .filter((entry) => (entry.type === "text" || entry.type === "thinking") && entry.content?.trim())
    .reverse(), [messages.data]);
  const replies = collaborator.messages;
  const failed = isAgent && (history.isError || messages.isError);

  return (
    <div className="space-y-3 text-left">
      <div>
        <div className="flex items-baseline justify-between gap-2">
          <h3 className="min-w-0 break-words text-body font-semibold">{collaborator.messages[0]?.actor_name || getActorName(collaborator.actorType, collaborator.actorId)}</h3>
          <span className="shrink-0 text-caption text-muted-foreground">{t(($) => $.tree.message_count, { count: replies.length })}</span>
        </div>
        <p className="break-words text-caption text-muted-foreground">{issue.identifier} · {issue.title}</p>
      </div>
      <div className="max-h-[min(28rem,55vh)] space-y-4 overflow-auto overscroll-contain break-words">
        {entries.map((entry) => (
          <div key={entry.seq}>
            <p className="mb-1 text-caption font-medium text-muted-foreground">
              {entry.type === "thinking" ? t(($) => $.inline_run.thinking) : t(($) => $.tree.agent_reply)}
              {entry.created_at && <> · {timeAgo(entry.created_at)}</>}
            </p>
            <RichContent content={entry.content!} />
          </div>
        ))}
        {replies.map((reply) => (
          <div key={reply.id}>
            <p className="mb-1 text-caption font-medium text-muted-foreground">{timeAgo(reply.created_at)}</p>
            {reply.content && <RichContent content={reply.content} />}
            <AttachmentList attachments={reply.attachments} content={reply.content} />
          </div>
        ))}
        {failed && <Button variant="outline" size="sm" onClick={() => {
          if (history.isError) void history.refetch();
          if (messages.isError) void messages.refetch();
        }}>{t(($) => $.status_catalog_error.retry)}</Button>}
      </div>
    </div>
  );
}
