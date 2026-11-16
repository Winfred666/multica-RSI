import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, expect, it, vi } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { SOPCommentContent, SOPRawToggle } from "./sop-comment-content";

const { preview, createComment, subscribe, reconnect } = vi.hoisted(() => ({
  preview: vi.fn(), createComment: vi.fn(), subscribe: vi.fn(), reconnect: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({ api: { getCommentSubIssuePreview: preview, createComment } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("@multica/core/realtime", () => ({ useWSEvent: subscribe, useWSReconnect: reconnect }));
vi.mock("../../editor", () => ({ ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div> }));

const questionId = "11111111-1111-4111-8111-111111111111";
const href = `/acme/issues/source#comment-${questionId}`;
const prose = `For the owner.\n\n[Question](${href})`;
const output = { status: "review", instruction: "Approve this change?", next_step: [
  { instruction: "Approve", command: "multica sop run review approve" },
] };
const render = (answered = false) => `!sop-render review {}\n${JSON.stringify({ ...output, ...(answered ? { args: ["approve"] } : {}) })}`;
const entry = (content: string): TimelineEntry => ({
  id: "forward", type: "comment", actor_type: "agent", actor_id: "agent", created_at: "2026-11-14T10:00:00Z", content,
});
const sourceComment = (id: string, content: string) => ({
  id, content, parent_id: null, type: "comment", author: { type: "agent", id: "agent", name: "Renderer" },
  created_at: "2026-11-14T10:00:00Z", updated_at: "2026-11-14T10:00:00Z", revision: 1, attachments: [],
});
function mount(content: string, raw = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return renderWithI18n(<QueryClientProvider client={client}>
    <SOPRawToggle entry={entry(content)} pressed={raw} onClick={() => {}} />
    <SOPCommentContent issueId="recipient" entry={entry(content)} currentUserId="member" raw={raw} />
  </QueryClientProvider>);
}
beforeEach(() => {
  vi.clearAllMocks();
  preview.mockResolvedValue({ source_issue: { id: "source", title: "Source title" }, anchor_comment_id: questionId,
    comment_thread: [sourceComment("earlier", "Prior source-thread context"), sourceComment(questionId, render())] });
  createComment.mockResolvedValue({ id: "answer" });
});

it("renders pending source-thread context and submits using the source question, not the forward", async () => {
  const { container } = mount(prose);
  expect(await screen.findByText("Prior source-thread context")).toBeVisible();
  expect(screen.getByRole("link", { name: "Source title" })).toHaveAttribute("href", href);
  expect(preview).toHaveBeenCalledWith(questionId);
  expect(container.querySelector("[data-sop-source-window]")).toHaveClass("overflow-y-auto", "overscroll-contain");
  fireEvent.click(screen.getByRole("radio", { name: "Approve" }));
  fireEvent.click(screen.getByRole("button", { name: "Submit" }));
  await waitFor(() => expect(createComment).toHaveBeenCalledWith("source",
    `!sop-remote review {"args":["approve"],"que_comment_id":"${questionId}"}`,
    undefined, questionId, undefined, undefined, undefined));
});

it("mirrors source updates and uses the same questionnaire renderer for delivered answers", async () => {
  const mirror = mount(prose);
  await screen.findByText("Prior source-thread context");
  preview.mockResolvedValue({ source_issue: { id: "source", title: "Source title" }, anchor_comment_id: questionId,
    comment_thread: [sourceComment("earlier", "Prior source-thread context"), sourceComment(questionId, render(true))] });
  const updated = subscribe.mock.calls.findLast(([event]) => event === "comment:updated")![1];
  updated({ comment: { issue_id: "source" } });
  expect(await screen.findByText("Answered")).toBeVisible();
  expect(screen.getByRole("radio", { name: "Approve" })).toBeChecked();
  expect(screen.getByRole("radio", { name: "Approve" })).toBeDisabled();
  expect(mirror.container.querySelector("[data-sop-source-window]")).not.toBeNull();
  mirror.unmount();
  mount(render(true));
  expect(screen.getByText("Answered")).toBeVisible();
  expect(screen.getByRole("radio", { name: "Approve" })).toBeChecked();
  expect(screen.getByRole("radio", { name: "Approve" })).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Submit" })).toBeNull();
});

it("keeps the pending forward available as raw content without loading its mirror", () => {
  const { container } = mount(prose, true);
  expect(container.querySelector("pre")?.textContent).toBe(prose);
  expect(screen.getByRole("button", { name: "View source" })).toHaveAttribute("aria-pressed", "true");
  expect(preview).not.toHaveBeenCalled();
  expect(subscribe).not.toHaveBeenCalled();
});
