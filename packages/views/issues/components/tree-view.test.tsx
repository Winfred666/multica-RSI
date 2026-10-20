import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createStore } from "zustand/vanilla";
import { setApiInstance, type ApiClient } from "@multica/core/api";
import { viewStoreSlice } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import type { AgentTask, Issue, TimelineEntry, TaskMessagePayload } from "@multica/core/types";
import { chatKeys } from "@multica/core/chat/queries";
import { issueTasksOptions, issueTimelineOptions } from "@multica/core/issues/queries";
import { renderWithI18n } from "../../test/i18n";
import { TreeView } from "./tree-view";
import type { IssueStatusPagination } from "../surface/use-issue-status-branches";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: (_type: string, id: string) => id }) }));
vi.mock("@multica/core/issue-statuses/hooks", () => {
  const catalog = { statuses: [], activeStatuses: [], isLoaded: true, colorOf: () => null, iconOf: () => null };
  return { useIssueStatuses: () => catalog };
});
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/issues/${id}` }) }));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ push: vi.fn() }), AppLink: ({ href, children, className }: { href: string; children: ReactNode; className?: string }) => <a href={href} className={className}>{children}</a> }));
vi.mock("../actions", () => ({ IssueActionsContextMenu: ({ children }: { children: ReactNode }) => children }));
vi.mock("./issue-agent-activity-indicator", () => ({ IssueAgentActivityIndicator: () => null }));
vi.mock("./custom-status-chip", () => ({ CustomStatusChip: () => null, useIsCustomStatus: () => false }));
vi.mock("./comment-card", () => ({ AttachmentList: () => null }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: ({ actorId }: { actorId: string }) => <span role="img" aria-label={actorId} /> }));
vi.mock("../../rich-content", () => ({ RichContent: ({ content }: { content: string }) => <p>{content}</p> }));

const issue = (id: string, parent: string | null = null, status = "todo") => ({
  id, title: id, identifier: id, status, parent_issue_id: parent, assignee_type: "agent", assignee_id: `agent-${id}`, position: 0,
  priority: "none", updated_at: "2026-01-01", labels: [], properties: {},
} as unknown as Issue);

const comment = (actor: string, content = "Posted reply", actorType = "agent"): TimelineEntry => ({
  type: "comment", id: content, actor_type: actorType, actor_id: actor, content, created_at: "2026-01-01",
});

function setup(rows: Issue[], tasks: AgentTask[] = [], timeline: TimelineEntry[] = []) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const api = {
    listTimeline: vi.fn<() => Promise<TimelineEntry[]>>(async () => timeline),
    getAgentTaskSnapshot: vi.fn(async () => tasks),
    listTasksByIssue: vi.fn(async () => tasks),
    listTaskMessages: vi.fn<() => Promise<TaskMessagePayload[]>>(async () => []),
    listProperties: vi.fn(async () => ({ properties: [] })),
  };
  setApiInstance(api as unknown as ApiClient);
  const store = createStore(viewStoreSlice);
  store.setState({ viewMode: "tree", hiddenStatuses: ["done", "cancelled"] });
  const create = vi.fn();
  const render = (issues: Issue[]) => <QueryClientProvider client={qc}><ViewStoreProvider store={store}><TreeView issues={issues} statusPagination={{} as IssueStatusPagination} onCreateIssue={create} /></ViewStoreProvider></QueryClientProvider>;
  const result = renderWithI18n(render(rows));
  return { ...result, qc, store, create, render, api };
}

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(800);
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(function (this: HTMLElement) {
    return this.hasAttribute("data-tree-card") ? 140 : 600;
  });
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

describe("TreeView", () => {
  it("expands collapsed issues, creates a child under its parent, and reacts to new filtered rows", async () => {
    const user = userEvent.setup();
    const root = issue("Parent");
    const done = issue("Delivered", "Parent", "done");
    const { create, rerender, render, qc } = setup([root, done]);
    expect(screen.queryByRole("link", { name: /Delivered/ })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Expand hidden issues (1)" }));
    expect(screen.getByRole("link", { name: /Delivered/ })).toBeInTheDocument();
    await user.click(screen.getAllByRole("button", { name: "Create sub-issue" })[0]!);
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ parent_issue_id: "Parent" }));
    rerender(render([root, done, issue("New sub-issue", "Parent")]));
    expect(screen.getByRole("link", { name: /New sub-issue/ })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Collapse hidden statuses" }));
    expect(screen.queryByRole("link", { name: /Delivered/ })).not.toBeInTheDocument();
    qc.clear();
  });

  it("supports zoom and panning with standard cards", async () => {
    const row = issue("Working");
    const tasks = ["agent-Working", "helper"].map((agent_id) => ({ agent_id, issue_id: row.id, status: "running", created_at: "2026-01-01" } as AgentTask));
    const { container, qc } = setup([row], tasks);
    await userEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    expect(screen.getByText("83%")).toBeInTheDocument();
    for (let i = 0; i < 7; i++) await userEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    const canvas = screen.getByRole("application", { name: "Tree" });
    canvas.setPointerCapture = vi.fn();
    canvas.hasPointerCapture = () => true;
    canvas.releasePointerCapture = vi.fn();
    const content = container.querySelector(".zoom-canvas-content") as HTMLElement;
    const previous = content.style.transform;
    fireEvent.pointerDown(canvas, { button: 0, pointerId: 1, clientX: 100, clientY: 100 });
    fireEvent.pointerMove(canvas, { pointerId: 1, clientX: 40, clientY: 50 });
    expect(content.style.transform).not.toBe(previous);
    fireEvent.pointerUp(canvas, { pointerId: 1 });
    act(() => qc.clear());
  });

  it("uses standard card display options and opens the latest report from its title in any status", async () => {
    const row = { ...issue("Review"), description: "Review content" };
    const { qc, api, store } = setup([row]);
    const key = issueTimelineOptions(row.id).queryKey;
    await waitFor(() => expect(qc.getQueryState(key)?.status).toBe("success"));
    const comments: TimelineEntry[] = Array.from({ length: 8 }, (_, index) => ({
      type: "comment", id: `comment-${index}`, actor_type: "agent", actor_id: `helper-${index}`,
      content: `Delivery report ${index}`, created_at: `2026-01-0${index + 1}`,
    }));
    api.listTimeline.mockResolvedValue(comments);
    act(() => qc.setQueryData(key, comments));
    await screen.findByRole("img", { name: "helper-7" });
    expect(screen.queryByRole("img", { name: "agent-Review" })).not.toBeInTheDocument();
    act(() => store.getState().toggleCardProperty("assignee"));
    expect(screen.queryByRole("img", { name: "helper-7" })).not.toBeInTheDocument();
    act(() => store.getState().toggleCardProperty("assignee"));
    act(() => store.getState().toggleCardProperty("description"));
    expect(screen.getByText("Review content")).toBeInTheDocument();
    act(() => store.getState().toggleCardProperty("description"));
    expect(screen.queryByRole("button", { name: "Latest agent report" })).not.toBeInTheDocument();
    await userEvent.hover(screen.getByText("Review", { selector: "span[tabindex]" }));
    expect(await screen.findByText("Delivery report 7")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Review" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Latest agent report" })).toHaveAttribute("aria-selected", "true");
    await userEvent.click(screen.getByRole("tab", { name: "Description" }));
    expect(screen.getByText("Review content")).toBeInTheDocument();
    expect(screen.queryByText("Delivery report 7")).not.toBeInTheDocument();
    qc.clear();
  });

  it("loads transcripts only on hover and updates live thinking and replies", async () => {
    const row = issue("Working");
    const task = { id: "10000000-0000-4000-8000-000000000001", agent_id: "agent-Working", issue_id: row.id, status: "running", created_at: "2026-01-01" } as AgentTask;
    const other = { ...task, id: "10000000-0000-4000-8000-000000000002", issue_id: "Elsewhere", created_at: "2026-02-01" };
    const { qc, api } = setup([row], [task, other], [comment("agent-Working")]);
    const message = (seq: number, type: TaskMessagePayload["type"], content: string): TaskMessagePayload => ({ task_id: task.id, issue_id: row.id, seq, type, content });
    api.listTaskMessages.mockResolvedValue([
      message(1, "thinking", "Checking "), message(2, "thinking", "the branch"),
      message(3, "tool_use", "Internal tool details"), message(4, "text", "Found the answer"),
    ]);
    expect(api.listTaskMessages).not.toHaveBeenCalled();
    await userEvent.hover(await screen.findByRole("img", { name: "agent-Working" }));
    expect(await screen.findByText("Checking the branch")).toBeInTheDocument();
    expect(screen.getByText("Found the answer")).toBeInTheDocument();
    expect(screen.getByText("Thinking")).toBeInTheDocument();
    expect(screen.queryByText("Internal tool details")).not.toBeInTheDocument();
    expect(api.listTaskMessages).toHaveBeenCalledWith(task.id);
    expect(api.listTaskMessages).not.toHaveBeenCalledWith(other.id);
    act(() => qc.setQueryData(chatKeys.taskMessages(task.id), [message(5, "text", " with live updates")]));
    expect(await screen.findByText("Found the answer with live updates")).toBeInTheDocument();
    qc.clear();
  });

  it("finds an agent's past issue run even when the workspace snapshot has moved on", async () => {
    const row = issue("Past");
    const { qc, api } = setup([row], [], [comment("agent-Past")]);
    const task = { id: "10000000-0000-4000-8000-000000000003", agent_id: "agent-Past", issue_id: row.id, status: "completed", created_at: "2026-01-01" } as AgentTask;
    api.listTasksByIssue.mockResolvedValue([task]);
    api.listTaskMessages.mockResolvedValue([{ task_id: task.id, issue_id: row.id, seq: 1, type: "text", content: "Previous run reply" }]);
    await userEvent.hover(await screen.findByRole("img", { name: "agent-Past" }));
    expect(await screen.findByText("Previous run reply", {}, { timeout: 3000 })).toBeInTheDocument();
    expect(api.listTaskMessages).toHaveBeenCalledWith(task.id);
    qc.clear();
  });

  it("shows direct agent replies and excludes other agents and deleted comments", async () => {
    const row = issue("Replies");
    const { qc } = setup([row], [], [
      { type: "comment", id: "reply", actor_type: "agent", actor_id: "agent-Replies", content: "Direct reply", created_at: "2026-01-01" },
      { type: "comment", id: "other", actor_type: "agent", actor_id: "helper", content: "Another agent reply", created_at: "2026-01-02" },
      { type: "comment", id: "deleted", actor_type: "agent", actor_id: "agent-Replies", content: "Deleted reply", created_at: "2026-01-03", deleted_at: "2026-01-04" },
    ]);
    await userEvent.hover(await screen.findByRole("img", { name: "agent-Replies" }));
    expect(await screen.findByText("Direct reply")).toBeInTheDocument();
    expect(screen.queryByText("Another agent reply")).not.toBeInTheDocument();
    expect(screen.queryByText("Deleted reply")).not.toBeInTheDocument();
    qc.clear();
  });

  it("shows an empty report, allows reading the description, and retries run history without hiding comments", async () => {
    const row = { ...issue("Empty"), description: "Original request" };
    const { qc, api } = setup([row]);
    await userEvent.hover(screen.getByText("Empty", { selector: "span[tabindex]" }));
    expect(await screen.findByText("No agent report yet")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("tab", { name: "Description" }));
    expect(screen.getByText("Original request")).toBeInTheDocument();
    await userEvent.unhover(screen.getByText("Empty", { selector: "span[tabindex]" }));
    api.listTasksByIssue.mockRejectedValueOnce(new Error("Unavailable"));
    act(() => qc.setQueryData(issueTimelineOptions(row.id).queryKey, [comment("agent-Empty")]));
    await userEvent.hover(await screen.findByRole("img", { name: "agent-Empty" }));
    const retry = await screen.findByRole("button", { name: "Try again" });
    await userEvent.click(retry);
    await waitFor(() => expect(api.listTasksByIssue).toHaveBeenCalledTimes(2));
    expect(screen.getByText("Posted reply")).toBeInTheDocument();
    qc.clear();
  });

  it("shows human collaborators by contribution frequency and grows their hover track with new messages", async () => {
    const row = issue("Human trace");
    const { qc, api, container } = setup([row], [], [comment("ceo", "First instruction", "member"), comment("helper")]);
    await userEvent.hover(await screen.findByRole("img", { name: "ceo" }));
    expect(await screen.findByText("First instruction")).toBeInTheDocument();
    expect(screen.getByText("1 message")).toBeInTheDocument();
    expect(api.listTaskMessages).not.toHaveBeenCalled();
    act(() => qc.setQueryData<TimelineEntry[]>(issueTimelineOptions(row.id).queryKey, (old = []) => [...old, { ...comment("ceo", "Second instruction", "member"), created_at: "2026-01-02" }]));
    expect(await screen.findByText("Second instruction")).toBeInTheDocument();
    expect(screen.getByText("2 messages")).toBeInTheDocument();
    const avatars = container.querySelector('[aria-label="Collaborators"]')!;
    expect(avatars.querySelector('[role="img"]')).toHaveAttribute("aria-label", "ceo");
    qc.clear();
  });

  it("shows server input trends before hover and refreshes the ring when run history changes", async () => {
    const row = issue("Trend");
    const run = { id: "run", agent_id: "agent-Trend", issue_id: row.id, status: "completed", created_at: "2026-01-01", input_trend: { current_context: 1000, context_compression_count: 1 } } as AgentTask;
    const { qc, api } = setup([row], [run], [comment("agent-Trend")]);
    const ring = await screen.findByRole("img", { name: "Current context: 1,000 tokens" });
    expect(ring.querySelector("svg")).toBeInTheDocument();
    expect(screen.getByLabelText("Context compressions: 1")).toHaveTextContent("1");
    expect(api.listTaskMessages).not.toHaveBeenCalled();
    act(() => qc.setQueryData(issueTasksOptions(row.id).queryKey, [{ ...run, input_trend: { ...run.input_trend!, current_context: 500, context_compression_count: 2 } }]));
    expect(await screen.findByRole("img", { name: "Current context: 500 tokens" })).toBeInTheDocument();
    expect(screen.getByLabelText("Context compressions: 2")).toHaveTextContent("2");
    await userEvent.hover(screen.getByRole("img", { name: "agent-Trend" }));
    expect(await screen.findByText("Estimated from completed runs; ring scaled to the largest recorded context.")).toBeInTheDocument();
    qc.clear();
  });
});
