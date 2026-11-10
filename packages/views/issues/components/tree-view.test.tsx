import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, type ReactNode } from "react";
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
import { IssueSurfaceSelectionProvider, useCreateIssueSurfaceSelection } from "../surface/selection-context";
import type { ZoomTransform } from "../../editor/hooks/use-zoom-canvas";

const wsHandlers = vi.hoisted(() => new Map<string, Set<(payload: unknown) => void>>());
vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (event: string, handler: (payload: unknown) => void) => {
    useEffect(() => {
      const handlers = wsHandlers.get(event) ?? new Set();
      wsHandlers.set(event, handlers);
      handlers.add(handler);
      return () => { handlers.delete(handler); };
    }, [event, handler]);
  },
  useWSReconnect: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: (_type: string, id: string) => id, getActorAvatarUrl: () => null }) }));
vi.mock("@multica/core/issue-statuses/hooks", () => {
  const catalog = { statuses: [], activeStatuses: [], isLoaded: true, colorOf: () => null, iconOf: () => null };
  return { useIssueStatuses: () => catalog };
});
vi.mock("@multica/core/paths", async (importOriginal) => ({ ...await importOriginal<typeof import("@multica/core/paths")>(), useWorkspacePaths: () => ({ issueDetail: (id: string) => `/issues/${id}` }) }));
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

function SelectionProvider({ children }: { children: ReactNode }) {
  const selection = useCreateIssueSurfaceSelection("tree");
  return <IssueSurfaceSelectionProvider selection={selection}>{children}</IssueSurfaceSelectionProvider>;
}

function setup(rows: Issue[], tasks: AgentTask[] = [], timeline: TimelineEntry[] = [], treeViewport: ZoomTransform | null = null) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const api = {
    listTimeline: vi.fn<() => Promise<TimelineEntry[]>>(async () => timeline),
    listAttachments: vi.fn(async () => []),
    getAgentTaskSnapshot: vi.fn(async () => tasks),
    listTasksByIssue: vi.fn(async () => tasks),
    listTaskMessages: vi.fn<() => Promise<TaskMessagePayload[]>>(async () => []),
    listProperties: vi.fn(async () => ({ properties: [] })),
  };
  setApiInstance(api as unknown as ApiClient);
  const store = createStore(viewStoreSlice);
  store.setState({ viewMode: "tree", hiddenStatuses: ["done", "cancelled"], treeViewport });
  const create = vi.fn();
  const render = (issues: Issue[]) => <QueryClientProvider client={qc}><ViewStoreProvider store={store}><SelectionProvider><TreeView issues={issues} statusPagination={{} as IssueStatusPagination} onCreateIssue={create} /></SelectionProvider></ViewStoreProvider></QueryClientProvider>;
  const result = renderWithI18n(render(rows));
  return { ...result, qc, store, create, render, api };
}

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockImplementation(function (this: HTMLElement) {
    return this.hasAttribute("data-tree-card") ? 300 : 800;
  });
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(function (this: HTMLElement) {
    return this.hasAttribute("data-tree-card") ? 140 : 600;
  });
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

describe("TreeView", () => {
  it("preserves packing on resize and repacks at the current width on issue creation or deletion", () => {
    let canvasWidth = 1200;
    vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockImplementation(function (this: HTMLElement) {
      return this.hasAttribute("data-tree-card") ? 300 : canvasWidth;
    });
    const observers: (() => void)[] = [];
    vi.stubGlobal("ResizeObserver", class {
      constructor(callback: () => void) { observers.push(callback); }
      observe() {}
      unobserve() {}
      disconnect() {}
    });
    try {
      const { container, qc } = setup(Array.from({ length: 6 }, (_, index) => issue(`Root ${index}`)));
      const rootRows = () => new Set([...container.querySelectorAll("[data-tree-card]")].map((card) => (card.parentElement as HTMLElement).style.top)).size;
      expect(rootRows()).toBe(2);
      canvasWidth = 390;
      act(() => observers.forEach((resize) => resize()));
      expect(rootRows()).toBe(2);
      act(() => wsHandlers.get("issue:created")?.forEach((handler) => handler({})));
      expect(rootRows()).toBe(3);
      canvasWidth = 1200;
      act(() => observers.forEach((resize) => resize()));
      expect(rootRows()).toBe(3);
      act(() => wsHandlers.get("issue:deleted")?.forEach((handler) => handler({})));
      expect(rootRows()).toBe(2);
      qc.clear();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("shows one timestamped frame per workspace with nested issue links and connectors behind cards", async () => {
    const child = "0c99f8e3-543b-4394-bfe8-a5b60141352e", nested = "1c99f8e3-543b-4394-bfe8-a5b60141352e", denied = "2c99f8e3-543b-4394-bfe8-a5b60141352e";
    const link = (id: string, workspace_id: string, target: string) => ({ ...issue(id), workspace_id, metadata: { sub_workspace_id: target } });
    const { qc, container, rerender, render, store, api } = setup([issue("Owner")], [], [comment("nested-reviewer")]);
    const replay = { ...link("Replay", child, nested), status: "done", description: "Evaluate the selected branch", labels: [{ id: "evaluation", name: "Evaluation", color: "#529879" } as NonNullable<Issue["labels"]>[number]] };
    act(() => store.setState({ cardProperties: { ...store.getState().cardProperties, description: true, labels: true } }));
    act(() => { qc.setQueryData(["workspace-tree-snapshot", "ws-1"], {
      [child]: { workspace: { id: child, slug: "playground", name: "Playground" }, issues: [replay, { ...issue("Reviewed", "Replay", "done"), workspace_id: child }], capturedAt: new Date(2026, 10, 6, 12, 34, 56).getTime() },
      [nested]: { workspace: { id: nested, slug: "nested", name: "Nested" }, issues: [link("Nested issue", nested, child)], capturedAt: Date.now() },
      [denied]: { issues: [], capturedAt: Date.now() },
    }); });
    rerender(render([link("Owner", "ws-1", child), link("Other owner", "ws-1", child), link("Denied owner", "ws-1", denied)]));
    expect(screen.getByRole("link", { name: "Playground workspace, static at 11-06 12:34:56" })).toHaveAttribute("href", "/playground/issues");
    expect(screen.getByRole("link", { name: /Nested workspace, static at/ })).toHaveAttribute("href", "/nested/issues");
    expect(screen.getByRole("link", { name: /Replay/ })).toHaveAttribute("href", "/playground/issues/Replay");
    expect(screen.getByRole("link", { name: /Nested issue/ })).toHaveAttribute("href", "/nested/issues/Nested%20issue");
    expect(screen.getByRole("img", { name: "Workspace unavailable" })).toBeInTheDocument();
    expect(container.querySelectorAll(".zoom-canvas-content > svg rect")).toHaveLength(3);
    expect(container.querySelectorAll(".zoom-canvas-content > svg path.text-muted-foreground")).toHaveLength(5);
    expect(container.querySelector(".zoom-canvas-content > svg path.text-info")).toBeInTheDocument();
    expect(screen.getByText("Evaluate the selected branch")).toBeInTheDocument();
    expect(screen.getByText("Evaluation")).toBeInTheDocument();
    const replayCard = within(container.querySelector('[data-tree-card="Replay"]') as HTMLElement);
    expect(await replayCard.findByLabelText("nested-reviewer", { selector: "span[tabindex]" })).toBeInTheDocument();
    expect(api.listTimeline).toHaveBeenCalledWith("Replay", "playground");
    expect(api.listTasksByIssue).toHaveBeenCalledWith("Replay", "playground");
    act(() => store.setState({ cardProperties: { ...store.getState().cardProperties, description: false, labels: false } }));
    expect(screen.queryByText("Evaluate the selected branch")).not.toBeInTheDocument();
    expect(screen.queryByText("Evaluation")).not.toBeInTheDocument();
    expect(container.querySelector(".zoom-canvas-content > svg")!.compareDocumentPosition(screen.getByRole("link", { name: /Replay/ }))).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    act(() => qc.clear());
  });

  it("expands collapsed issues, creates a child under its parent, and reacts to new filtered rows", async () => {
    const user = userEvent.setup();
    const root = issue("Parent");
    const done = { ...issue("Delivered", "Parent", "done"), description: "Completed parent summary" };
    const cancelled = issue("Cancelled", "Parent", "cancelled");
    const descendant = issue("Active descendant", "Delivered");
    const { create, rerender, render, qc, store } = setup([root, done, issue("Other completed", "Parent", "done"), cancelled, descendant]);
    expect(screen.queryByRole("link", { name: /Active descendant/ })).not.toBeInTheDocument();
    act(() => store.getState().toggleCardProperty("description"));
    expect(screen.getByText("Completed parent summary")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Delivered/ })).not.toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole("application", { name: "Tree" }), { key: "a", ctrlKey: true });
    await user.click(screen.getByRole("button", { name: "Expand hidden issues (2)" }));
    expect(screen.getByRole("link", { name: /Delivered/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Active descendant/ })).toBeInTheDocument();
    await user.click(screen.getAllByRole("button", { name: "Create sub-issue" })[0]!);
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ parent_issue_id: "Parent" }));
    rerender(render([root, done, issue("Other completed", "Parent", "done"), cancelled, descendant, issue("New sub-issue", "Parent")]));
    expect(screen.getByRole("link", { name: /New sub-issue/ })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Collapse hidden statuses" }));
    expect(screen.queryByRole("link", { name: /Delivered/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Active descendant/ })).not.toBeInTheDocument();
    qc.clear();
  });

  it("supports zoom and Space panning over cards without opening them", async () => {
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
    fireEvent.keyDown(canvas, { key: " " });
    fireEvent.pointerDown(screen.getByRole("link", { name: /Working/ }), { button: 0, pointerId: 1, clientX: 100, clientY: 100 });
    fireEvent.pointerMove(canvas, { pointerId: 1, clientX: 40, clientY: 50 });
    expect(content.style.transform).not.toBe(previous);
    fireEvent.pointerUp(canvas, { pointerId: 1 });
    fireEvent.keyUp(canvas, { key: " " });
    expect(fireEvent.click(screen.getByRole("link", { name: /Working/ }))).toBe(false);
    const afterPan = content.style.transform;
    fireEvent.pointerDown(canvas, { button: 0, pointerId: 2, clientX: 100, clientY: 100 });
    fireEvent.pointerMove(canvas, { pointerId: 2, clientX: 40, clientY: 50 });
    expect(content.style.transform).toBe(afterPan);
    fireEvent.pointerUp(canvas, { pointerId: 2 });
    act(() => qc.clear());
  });

  it("rectangle-selects visible issues at the current zoom, supports additive selection, and clears with Escape", () => {
    vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockImplementation(function (this: HTMLElement) {
      return this.hasAttribute("data-tree-card") ? 300 : 6000;
    });
    const { container, qc, store } = setup([issue("First"), issue("Second"), issue("Third"), issue("Hidden", null, "done"), issue("Hidden too", null, "done")], [], [], { scale: 0.5, x: 80, y: 35 });
    const canvas = screen.getByRole("application", { name: "Tree" });
    const selected = () => [...container.querySelectorAll("[data-tree-card]:has([data-selected])")].map((card) => card.getAttribute("data-tree-card"));
    const content = container.querySelector(".zoom-canvas-content") as HTMLElement;
    const previous = content.style.transform;
    const camera = store.getState().treeViewport!;
    expect(camera.scale).toBe(0.5);
    const clientPoint = (x: number, y: number) => ({ clientX: camera.x + x * camera.scale, clientY: camera.y + y * camera.scale });
    // jsdom has no layout; selection reads the rendered bounds after camera restoration.
    for (const element of container.querySelectorAll<HTMLElement>("[data-tree-issue]")) {
      const card = element.closest<HTMLElement>("[data-tree-card]")!;
      const { left, top } = card.parentElement!.style;
      const point = clientPoint(parseFloat(left), parseFloat(top));
      vi.spyOn(element, "getBoundingClientRect").mockReturnValue(new DOMRect(point.clientX, point.clientY, card.offsetWidth * camera.scale, card.offsetHeight * camera.scale));
    }
    fireEvent.pointerDown(canvas, { button: 0, pointerId: 1, ...clientPoint(700, 200) });
    fireEvent.pointerMove(canvas, { pointerId: 1, ...clientPoint(40, 40) });
    expect(selected()).toEqual(["First", "Second"]);
    expect(container.querySelector("[data-tree-selection]")).toHaveStyle({ left: "40px", top: "40px", width: "660px", height: "160px" });
    expect(container.querySelector("[data-selected] > span[aria-hidden]")).toHaveClass("border", "border-brand", "pointer-events-none");
    expect(content.style.transform).toBe(previous);
    fireEvent.pointerMove(canvas, { pointerId: 1, ...clientPoint(400, 40) });
    expect(selected()).toEqual(["Second"]);
    fireEvent.pointerUp(canvas, { pointerId: 1 });
    expect(container.querySelector("[data-tree-selection]")).toBeNull();
    fireEvent.click(canvas);
    expect(selected()).toEqual(["Second"]);

    fireEvent.click(screen.getByRole("link", { name: /Third/ }));
    expect(selected()).toEqual(["Second", "Third"]);
    fireEvent.pointerDown(canvas, { button: 0, pointerId: 2, ...clientPoint(40, 40), shiftKey: true });
    fireEvent.pointerMove(canvas, { pointerId: 2, ...clientPoint(360, 200) });
    fireEvent.pointerUp(canvas, { pointerId: 2 });
    fireEvent.click(canvas);
    expect(selected()).toEqual(["First", "Second", "Third"]);

    fireEvent.keyDown(canvas, { key: "Escape" });
    expect(selected()).toEqual([]);
    fireEvent.keyDown(canvas, { key: "a", ctrlKey: true });
    expect(selected()).toEqual(["First", "Second", "Third"]);
    const first = screen.getByRole("link", { name: /First/ });
    const navigate = vi.fn((event: Event) => event.preventDefault());
    first.addEventListener("click", navigate);
    fireEvent.click(first);
    expect(selected()).toEqual(["Second", "Third"]);
    expect(navigate).not.toHaveBeenCalled();
    fireEvent.click(canvas);
    expect(selected()).toEqual([]);
    fireEvent.click(first);
    expect(navigate).toHaveBeenCalledOnce();
    qc.clear();
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
    expect(api.listTaskMessages).toHaveBeenCalledWith(task.id, undefined);
    expect(api.listTaskMessages).not.toHaveBeenCalledWith(other.id, undefined);
    act(() => qc.setQueryData(chatKeys.taskMessages(task.id), [message(5, "text", " with live updates")]));
    expect(await screen.findByText("Found the answer with live updates")).toBeInTheDocument();
    qc.clear();
  });

  it("updates collaborators and the open report on comment events without refetching", async () => {
    const row = issue("Working");
    const { qc, api } = setup([row, issue("Elsewhere")]);
    await waitFor(() => expect(api.listTimeline).toHaveBeenCalledTimes(2));
    await userEvent.hover(screen.getByText("Working", { selector: "span[tabindex]" }));
    expect(await screen.findByRole("heading", { name: "Working" })).toBeInTheDocument();
    const emit = (event: string, payload: unknown) => act(() => {
      for (const handler of wsHandlers.get(event) ?? []) handler(payload);
    });
    const reply = {
      id: "live-reply", issue_id: row.id, author_type: "agent", author_id: "live-agent",
      content: "New agent report", type: "comment", revision: 1, created_at: "2026-01-02",
    };
    emit("comment:created", { comment: reply });
    expect(await screen.findByRole("img", { name: "live-agent" })).toBeInTheDocument();
    expect(await screen.findByText("New agent report")).toBeInTheDocument();
    expect(qc.getQueryData(issueTimelineOptions("Elsewhere").queryKey)).toEqual([]);

    emit("comment:updated", { comment: { ...reply, revision: 2, content: "Updated agent report" } });
    expect(await screen.findByText("Updated agent report")).toBeInTheDocument();
    expect(screen.queryByText("New agent report")).not.toBeInTheDocument();

    emit("comment:deleted", { issue_id: row.id, comment_id: reply.id });
    await waitFor(() => expect(screen.queryByRole("img", { name: "live-agent" })).not.toBeInTheDocument());
    expect(screen.queryByText("Updated agent report")).not.toBeInTheDocument();
    expect(api.listTimeline).toHaveBeenCalledTimes(2);
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
    expect(api.listTaskMessages).toHaveBeenCalledWith(task.id, undefined);
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

  it("scales context against 256K and refreshes the ring when run history changes", async () => {
    const row = issue("Trend");
    const run = { id: "run", agent_id: "agent-Trend", issue_id: row.id, status: "completed", created_at: "2026-01-01", input_trend: { current_context: 128000, context_compression_count: 1 } } as AgentTask;
    const { qc, api } = setup([row], [run], [comment("agent-Trend")]);
    const ring = await screen.findByRole("img", { name: "Context: 128,000 / 256,000 tokens" });
    const arc = ring.querySelector("circle[stroke-dashoffset]")!;
    const circumference = Number(arc.getAttribute("stroke-dasharray"));
    expect(Number(arc.getAttribute("stroke-dashoffset"))).toBeCloseTo(circumference / 2);
    expect(screen.getByLabelText("Compaction: 1")).toHaveTextContent("1");
    expect(api.listTaskMessages).not.toHaveBeenCalled();
    act(() => qc.setQueryData(issueTasksOptions(row.id).queryKey, [{ ...run, input_trend: { ...run.input_trend!, current_context: 64000, context_compression_count: 2 } }]));
    expect(await screen.findByRole("img", { name: "Context: 64,000 / 256,000 tokens" })).toBeInTheDocument();
    expect(screen.getByLabelText("Compaction: 2")).toHaveTextContent("2");
    expect(Number(arc.getAttribute("stroke-dashoffset"))).toBeCloseTo(circumference * 0.75);
    await userEvent.hover(screen.getByRole("img", { name: "agent-Trend" }));
    expect(await screen.findByText("Context: 64,000 / 256,000 tokens")).toBeInTheDocument();
    expect(screen.getByText("Compaction: 2")).toBeInTheDocument();
    act(() => qc.setQueryData(issueTasksOptions(row.id).queryKey, [{ ...run, input_trend: { ...run.input_trend!, current_context: 512000 } }]));
    await waitFor(() => expect(Number(arc.getAttribute("stroke-dashoffset"))).toBe(0));
    await userEvent.hover(screen.getByRole("img", { name: "agent-Trend" }));
    expect(await screen.findByText("Estimated from completed runs; ring assumes a 256K-token context window.")).toBeInTheDocument();
    qc.clear();
  });
});
