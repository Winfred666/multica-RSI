---
name: multica-surface-block
description: Confirm and describe an external blocker before marking the current issue blocked.
---

Use before marking the current issue blocked:

```sh
python3 <this-skill-directory>/wrapper.py
```

Answer the returned questions in the ordinary issue context, then follow their callbacks. The caller owns the answers and recovery. Only `continue` sets `blocked --no-start` and notifies the parent. Resume an interrupted call to reconcile its native notification.
