# Shared configuration JSON artifacts

The wrapper and `sop.py` follow [R-328's accepted runtime](https://github.com/Winfred666/multica-featssship/tree/c396c654f245e2dd4d0686f70a16a73101ac4188/installation).
Only the needed configuration/reference and comment-artifact helpers are included
in `common.py`; no featssship controller, Git operation or delegation machinery is imported.

## Configuration first

Use `write_config(reference, payload, parent, message)` for shared JSON documents.
It uploads ordinary multipart `file` plus optional `attachment_id`, without owner
fields or an independent-mode flag. Backend alone chooses renewal/history; always
retain its returned ID. New config artifacts are ownerless and reusable by multiple
readers. `write_config` updates `reference['attachment_id']` and returns the native
announcement comment, preserving the source helper's return convention. The
announcement contains a stable Markdown link, not an owning comment attachment.

Persist the updated reference with the existing `link_config` helper before
other readers resume. The reference retains the source's `issue_id`/`filename`
fields for announcement routing and adds the canonical attachment ID. It is a
small shared pointer, not a configuration payload or a deliverables relationship list:

```python
from common import config_ref, link_config, read_config, write_config

reference = {'issue_id': context['issue_id'], 'filename': 'master.json'}
write_config(reference, {'repository': '...', 'directions': []}, parent='...')
link_config(context['issue_id'], reference)  # source key: featssship.config
payload = read_config(config_ref(context['cli']('get', context['issue_id'])))
```

The same pattern applies to delegation JSON. Its `source` points to the master
reference and its `issues` array contains the delegated settings. MAIN and the
developer/reviewer consume shared references through `config_ref`/`read_config`;
`DELEGATION_KEY` preserves the source's `featssship.delegation` key and optional
`index` field. They do not store duplicate JSON payloads in issue metadata.
Only these pointers and transient `sop.<skill-name>` runtime/recovery state remain
metadata. The package's local `config.json` still controls DEBUG as in R-328.

`read_config`/`download_config` also accept the source's existing owner/filename
references. Resolving one captures its current ID in the reference before a
subsequent update. Persist the refreshed pointer after writing; the backend may
return a different ID for a dependent file. Do not bulk-convert existing artifacts,
change old ownership, or update other consumers silently. Coordinate shared writers.
Keep credentials out of configuration payloads and tracked files.

If uploading, announcing or saving a reference fails, inspect the current file
and saved pointer before retrying; do not replay a completed workflow action.
No automatic delete/recreate or cleanup of existing metadata/configurations occurs.

## HTML and commands

Auto-compiled DEBUG HTML uses `common.write_artifact`'s ownerless upload path
and posts a non-owning `!file[label](returned markdown_url)` link in the current
issue thread (or an existing root). It omits issue/comment/task upload fields,
avoiding the CLI's ambient task ownership. Controller-supplied artifacts keep
R-328's ordinary comment attachments. Publication remains best effort, including
after completion; DEBUG settings and publication triggers are unchanged.

The minimal client contract is:

```sh
multica attachment upload FILE
multica attachment upload FILE --attachment-id ID
multica attachment get ID
multica attachment download ID -o DIRECTORY
```

Upload creates; `--attachment-id` selects the update target. `get` returns metadata
and URLs, not text extraction/OCR. `download` saves the actual text/image bytes.
The template uses the existing HTTP upload/read API for configs and ordinary
CLI comment/download operations; it does not require an installed CLI with the
new upload flags. See [the accepted backend contract](../../../../docs/engineering/fork-development.md#attachment-ownership-backend-p1).
No independent CLI/API flag, manual `metadata.deliverables` list or link/unlink wrapper is used.

This change is creator/template-only. Human approval is required before mechanical
propagation to other symbolic SoPs or workspace installs.
