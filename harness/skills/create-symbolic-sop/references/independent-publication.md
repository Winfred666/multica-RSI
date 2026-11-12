# Independent HTML publication

Use this contract for live SoP HTML and DEBUG previews. It does not change
comment-bound delivery (`issue comment add --attachment`) or ordinary uploads.
Only resources explicitly created with `independent=true` may be replaced without
history. Do not convert, reuse or relink ordinary comment/task/issue attachments.
Backend contract: [P1](../../../../docs/engineering/fork-development.md#independent-deliverables-backend-p1).

## Template publication

The wrapper uploads with `independent=true`, no owner fields, and the native
workspace header. A new upload returns an independent UUID. It saves that ID in
issue metadata `sop_artifacts`, a **string encoding** this object:

```json
{"multica-example":{"multica-example-state.html":"<independent-uuid>"}}
```

The skill/filename pair is a local publication slot, not resource identity.
Later callbacks read this explicit ID and send `attachment_id=<uuid>` with
`independent=true`. The backend rejects ordinary or foreign-workspace IDs; the
wrapper never falls back to filename matching, conversion, deletion or recreation.
It verifies the response's independent flag and requested ID.

After saving identity, the wrapper reads/modifies/writes the issue's `deliverables`
string-encoded UUID array, preserving other IDs and avoiding duplicates. This is
only a link, not an attachment owner. The resource can link to multiple issues or
none. Transient `sop.<skill-name>` cleanup never removes `sop_artifacts`, links or
content. A subsequent run uses the same ID. Read/modifying these whole metadata
keys requires coordination between writers; existing 8 KB/50-key limits apply.

Publication is best effort and never changes a completed workflow result. If
linking fails after identity is saved, the next publication replaces the same ID
and retries the link. If creation or saving identity has an uncertain outcome,
inspect workspace discovery before retrying. Reconcile the reported resource ID
into `sop_artifacts` rather than creating another object. Do not rerun a completed
action to repair publication. Missing/deleted or ineligible IDs require explicit
reconciliation; they do not trigger automatic recreation.

## Canonical CLI contract

This is the command contract for the CLI implementation, not a claim that the
installed CLI already supports the new flags/commands. Use the existing backend
API for template validation until that implementation is accepted.
Use the selected profile/native workspace or explicit global `--workspace-id`.

| Operation | Exact command | Behavior |
| --- | --- | --- |
| Create | `multica attachment upload FILE --independent --output json` | New unlinked independent resource; returns AttachmentResponse including `id`, `independent:true`, `markdown_url`. |
| Discover | `multica attachment list --independent --output json` | JSON array of all independent resources in the workspace, including unlinked ones, from workspace `?include=deliverables`. |
| Read | `multica attachment get ID --output json` | Existing attachment metadata response; check `independent` before treating it as independent. |
| Read bytes | `multica attachment download ID -o DIRECTORY` | Existing command; current bytes only. |
| Replace | `multica attachment upload FILE --independent --attachment-id ID --output json` | Same ID/storage key/markdown URL; overwrite current bytes, no content history. |
| Read links | `multica issue get ISSUE --output json` | Read `metadata.deliverables`, default missing to the string `[]`. |
| Link/unlink | `multica issue metadata set ISSUE --key deliverables --type string --value "$IDS" --output json` | Write the complete modified UUID array, encoded as a string. |
| Unlink all | `multica issue metadata delete ISSUE --key deliverables --output json` | Remove all links on that issue; resource bytes and other issues' links survive. |

Independent upload must suppress ambient task/issue/chat ownership. Explicit
owner flags such as `--task` conflict with `--independent`; `--attachment-id`
requires `--independent`. Upload without that flag retains its existing behavior.
New commands/flags must not change comment attachment publication.
Persist `id` for replacement and `markdown_url` for durable citations, never a
signed `download_url`. Same filename without an ID creates another resource.
No new link/unlink command or delete/recreate workaround is needed.

After creating a resource, capture its `id` as `ID`. This example links it to a
second issue without discarding that issue's other resources (Python 3 required):

```sh
IDS=$(multica issue get "$OTHER_ISSUE" --output json | python3 -c '
import json, sys
ids = json.loads(json.load(sys.stdin)["metadata"].get("deliverables", "[]"))
print(json.dumps(list(dict.fromkeys([*ids, sys.argv[1]]))))
' "$ID") &&
multica issue metadata set "$OTHER_ISSUE" --key deliverables --type string --value "$IDS" --output json
```

To unlink only that ID, use the same read/modify/write with:

```python
print(json.dumps([value for value in ids if value != sys.argv[1]]))
```

Unlinking never deletes content. Publishing again on the original SoP issue
restores its link using the retained slot binding. Explicit resource deletion is
a separate attachment API operation; it does not rewrite links or authorize
replacement with an ordinary object. No previous revision can be fetched or
recovered. Storage-provider object version retention must be disabled separately
when no-history storage is required.

Only this creator and template are changed here. Human approval is required
before mechanical propagation to existing symbolic SoPs or workspace installs.
