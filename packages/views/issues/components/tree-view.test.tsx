import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createStore } from "zustand/vanilla";
import { setApiInstance, type ApiClient } from "@multica/core/api";
import { viewStoreSlice } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import type { AgentTask, Issue, TimelineEntry } from "@multica/core/types";
import { issueTimelineOptions } from "@multica/core/issues/queries";
import { renderWithI18n } from "../../test/i18n";
import { TreeView } from "./tree-view";
import type { IssueStatusPagination } from "../surface/use-issue-status-branches";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: (_type: string, id: string) => id }) }));
vi.mock("@multica/core/issue-statuses/hooks", () => {
  const catalog = { statuses: [], activeStatuses: [], isLoaded: true, colorOf: () => null, iconOf: () => null };
  return { useIssueStatuses: () => catalog };
});
vi.mock("./issue-mention-card", () => ({ IssueMentionCard: ({ issue }: { issue: Issue }) => <a href={`/issues/${issue.id}`}>{issue.title}</a> }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: ({ actorId }: { actorId: string }) => <span role="img" aria-label={actorId} /> }));
vi.mock("../../rich-content", () => ({ RichContent: ({ content }: { content: string }) => <p>{content}</p> }));

const issue = (id: string, parent: string | null = null, status = "todo") => ({
  id, title: id, identifier: id, status, parent_issue_id: parent, assignee_type: "agent", assignee_id: `agent-${id}`, position: 0,
} as Issue);

function setup(rows: Issue[], tasks: AgentTask[] = []) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  setApiInstance({
    listTimeline: vi.fn(async () => []),
    getAgentTaskSnapshot: vi.fn(async () => tasks),
  } as unknown as ApiClient);
  const store = createStore(viewStoreSlice);
  store.setState({ hiddenStatuses: ["done", "cancelled"] });
  const create = vi.fn();
  const render = (issues: Issue[]) => <QueryClientProvider client={qc}><ViewStoreProvider store={store}><TreeView issues={issues} statusPagination={{} as IssueStatusPagination} onCreateIssue={create} /></ViewStoreProvider></QueryClientProvider>;
  const result = renderWithI18n(render(rows));
  return { ...result, qc, store, create, render };
}

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(800);
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(600);
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

describe("TreeView", () => {
  it("expands collapsed issues, creates a child under its parent, and reacts to new filtered rows", async () => {
    const user = userEvent.setup();
    const root = issue("Parent");
    const done = issue("Delivered", "Parent", "done");
    const { create, rerender, render, qc } = setup([root, done]);
    expect(screen.queryByRole("link", { name: "Delivered" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Expand hidden issues (1)" }));
    expect(screen.getByRole("link", { name: "Delivered" })).toBeInTheDocument();
    await user.click(screen.getAllByRole("button", { name: "Create sub-issue" })[0]!);
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ parent_issue_id: "Parent" }));
    rerender(render([root, done, issue("New sub-issue", "Parent")]));
    expect(screen.getByRole("link", { name: "New sub-issue" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Collapse hidden statuses" }));
    expect(screen.queryByRole("link", { name: "Delivered" })).not.toBeInTheDocument();
    qc.clear();
  });

  it("applies the existing border beam to every working collaborator and supports zoom and panning", async () => {
    const row = issue("Working");
    const tasks = ["agent-Working", "helper"].map((agent_id) => ({ agent_id, issue_id: row.id, status: "running", created_at: "2026-01-01" } as AgentTask));
    const { container, qc } = setup([row], tasks);
    await waitFor(() => expect(container.querySelectorAll(".border-beam-ring")).toHaveLength(2));
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

  it("keeps the main assignee visible with many helpers and previews the latest report", async () => {
    const row = issue("Review", null, "in_review");
    const { qc } = setup([row]);
    const key = issueTimelineOptions(row.id).queryKey;
    await waitFor(() => expect(qc.getQueryState(key)?.status).toBe("success"));
    const comments: TimelineEntry[] = Array.from({ length: 8 }, (_, index) => ({
      type: "comment", id: `comment-${index}`, actor_type: "agent", actor_id: `helper-${index}`,
      content: `Delivery report ${index}`, created_at: `2026-01-0${index + 1}`,
    }));
    act(() => qc.setQueryData(key, comments));
    const overflow = await screen.findByRole("button", { name: "Collaborating agents" });
    expect(screen.getByRole("img", { name: "agent-Review" })).toBeInTheDocument();
    expect(overflow).toHaveTextContent("+3");
    await userEvent.hover(screen.getByRole("button", { name: "Latest agent report" }));
    expect(await screen.findByText("Delivery report 7")).toBeInTheDocument();
    qc.clear();
  });
});
