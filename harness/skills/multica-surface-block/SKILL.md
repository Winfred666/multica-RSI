---
name: multica-surface-block
description: Confirm and describe an external blocker before marking the current issue blocked.
---

Use before marking the current issue blocked:

```sh
python3 <this-skill-directory>/wrapper.py
```

Answer the returned questions, then supply `--reason` with the concrete barrier and required help. The script posts that reply before marking the issue blocked; native steering asks the parent to help. Follow returned error guidance, reconcile writes before retrying, and use `repaired` once for assertion failures; report repeated failure to the parent.
