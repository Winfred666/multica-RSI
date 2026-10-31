// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "./schema";
import { IssueSnapshotSchema } from "./schemas";

const snapshot = {
  version: 1,
  captured_at: "2026-10-31T00:00:00Z",
  issue: {
    id: "issue-1", workspace_id: "ws-1", number: 1, identifier: "MUL-1",
    title: "Example", description: null, status: "backlog", priority: "none",
    assignee_type: null, assignee_id: null, creator_type: "member", creator_id: "user-1",
    parent_issue_id: null, project_id: null, position: 0, start_date: null, due_date: null,
    metadata: {}, properties: {}, labels: [], created_at: "2026-10-31", updated_at: "2026-10-31",
  },
  comments: [],
  attachments: [{
    id: "file-1", filename: "notes.txt", url: "/original.txt", download_url: "/download",
    comment_id: null, content_type: "text/plain", size_bytes: 5, data: "bm90ZXM=",
  }],
};

describe("issue snapshot response", () => {
  it("retains file bytes and original issue provenance", () => {
    const parsed = parseWithFallback<typeof snapshot | null>(snapshot, IssueSnapshotSchema, null, { endpoint: "snapshot" });
    expect(parsed?.attachments[0]?.data).toBe("bm90ZXM=");
    expect(parsed?.issue.id).toBe("issue-1");
  });

  it.each([
    { ...snapshot, version: 2 },
    { ...snapshot, comments: null },
    { ...snapshot, attachments: [{ ...snapshot.attachments[0], data: null }] },
    { ...snapshot, issue: { ...snapshot.issue, labels: ["malformed"] } },
  ])("rejects incomplete or unsupported snapshots", (input) => {
    expect(parseWithFallback(input, IssueSnapshotSchema, null, { endpoint: "snapshot" })).toBeNull();
  });
});
