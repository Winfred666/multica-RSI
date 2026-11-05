import { cleanup, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { ApiError } from "@multica/core/api";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { WorkspaceTreeFrame } from "./workspace-tree-frame";

vi.mock("../../navigation", () => ({
  AppLink: ({ href, children }: { href: string; children: ReactNode }) => <a href={href}>{children}</a>,
}));
afterEach(() => { cleanup(); vi.clearAllMocks(); });

it("shows a compact crossed-eye frame when membership is denied", () => {
  renderWithI18n(<WorkspaceTreeFrame error={new ApiError("Forbidden", 403, "Forbidden")} retry={vi.fn()} />);
  expect(screen.getByRole("img", { name: "Workspace unavailable" })).toBeInTheDocument();
  expect(screen.queryByRole("link")).not.toBeInTheDocument();
});

it("renders workspace and issue links in the child workspace", () => {
  const data = { workspace: { id: "ws", name: "Playground", slug: "playground" }, issues: [
    { id: "root", identifier: "P-1", title: "Replay", status: "todo" },
    { id: "child", parent_issue_id: "root", identifier: "P-2", title: "Child", status: "done" },
  ] as Issue[] };
  renderWithI18n(<WorkspaceTreeFrame data={data} error={null} retry={vi.fn()} />);
  expect(screen.getByRole("link", { name: "Playground" })).toHaveAttribute("href", "/playground/issues");
  expect(screen.getByRole("link", { name: /P-1.*Replay/ })).toHaveAttribute("href", "/playground/issues/root");
  expect(screen.getByRole("link", { name: /P-2.*Child/ })).toHaveAttribute("href", "/playground/issues/child");
});
