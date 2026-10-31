# Issue snapshot API

AIDE can collect and restore one issue through authenticated backend APIs. It owns the saved JSON and task-folder archive. Multica stores no snapshot records and needs no agent provider, CLI, or adapter to perform these operations.

## Export

`GET /api/issues/{id}/snapshot`, with `Authorization` and `X-Workspace-ID`, returns:

```json
{
  "version": 1,
  "captured_at": "2026-10-31T00:00:00Z",
  "issue": { "...": "existing issue response, including labels, metadata and properties" },
  "comments": [ { "...": "existing comment response" } ],
  "attachments": [ { "...": "existing attachment response", "data": "base64 file bytes" } ]
}
```

The backend reads issue fields, labels, comment threads (including tombstones and resolutions), and attachment references from one database transaction. It embeds attachment bytes so the document survives source deletion. If a file cannot be read, export fails instead of returning a partial snapshot.

Version 1 covers one issue. It excludes sub-issues, reactions, activities, subscribers, wakeups, captured source-context records, and execution history. AIDE captures task-folder files separately. Limits: 2,000 comments, 100 attachments, 8 MiB total attachment bytes, and 16 MiB encoded snapshot JSON. Oversized exports fail without truncation.

## Restore

`POST /api/issues/restore` accepts the exported document:

```json
{
  "snapshot": { "version": 1, "...": "the complete exported document" },
  "title": "Optional replacement title",
  "project_id": "optional target project UUID"
}
```

Restore requires membership in the snapshot's workspace. The original project is the default; `project_id: ""` restores without a project. Project, label, and property definitions must still exist in that workspace. Unknown or archived references fail through the existing create validation.

The response is HTTP 201 with the new issue. Restore allocates a new issue number and new issue, comment, and attachment IDs, reconnects comment parents and attachments, and rewrites attachment links in the description and comments. It retains title, description, priority, dates, labels, metadata, properties, comment content, tombstones, and resolved state. The issue and restored rows commit together; failed imports clean up uploaded files.

The new issue starts unassigned, without a parent, stage, task, or session. Its initial status is backlog, or the project's initial todo step when its workflow excludes backlog. Restore does not invoke comment triggers. AIDE can bind the restored task folder and request a fresh run through the existing APIs after it verifies the import.

Original author identities, timestamps, comment types, and task references remain available in the exported document as provenance. New rows belong to the authenticated restoring actor and receive new timestamps. System comments become ordinary comments; progress updates retain their type. Restore never accepts exported identities as authority to impersonate another author.

Restore accepts at most 17 MiB including its request envelope. Export uses HTTP 409 when it cannot collect a complete document and 413 when the encoded document exceeds 16 MiB. Invalid restore documents return 400; unavailable attachment storage returns 503.
