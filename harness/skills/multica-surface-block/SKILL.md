---
name: multica-surface-block
description: Confirm and describe an external blocker before marking the current issue blocked.
---

Use before marking the current issue blocked:

```sh
python3 <this-skill-directory>/wrapper.py
```

Answer the returned questions, then supply `--reason` with the concrete barrier and required help. For a child, also supply `--parent-thread ID` for the relevant request on its parent. The script posts the child blocker, marks it blocked, and sends its link and required help to the responsible parent with native steering. It reports completion only after dispatch is confirmed. A blocked child can resume an incomplete handoff. Keep the saved handoff when a write or dispatch is uncertain; inspect the native comment before retrying. Follow returned error guidance, reconcile writes before retrying, and use `repaired` once for assertion failures; report repeated failure to the parent.

`python3 <this-skill-directory>/wrapper.py --help` replays the last decision for
the native task, or shows this skill body for a new or completed task, without
running the procedure or creating a task record.
