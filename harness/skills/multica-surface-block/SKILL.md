---
name: multica-surface-block
description: Confirm and describe an external blocker before marking the current issue blocked.
---

Use before marking the current issue blocked:

```sh
python3 <this-skill-directory>/wrapper.py
```

Answer the returned questions, then supply `--reason` with only the concrete
blocker and exact help needed. For a child, use `--parent-thread ID` for the
original assignment thread on its parent when one exists; omit it only when
there is no assignment thread. The script marks the issue blocked and steers
one concise help request to that thread, or a new parent thread when absent.
It completes only after parent dispatch is confirmed. It posts no blocker note
or return receipt on the current issue. Follow `multica-issue-workflow` for replies.

A blocked child can resume an incomplete handoff. Keep the saved handoff when
a write or dispatch is uncertain; inspect the native comment before retrying.
Follow returned error guidance, reconcile writes before retrying, and use
`repaired` once for assertion failures; report repeated failure to the parent.

`python3 <this-skill-directory>/wrapper.py --help` replays the last decision for
this issue, or shows this skill body when no SoP record exists, without running
the procedure or creating state.
