---
name: review-child-work
description: Accept a reviewed child deliverable or send an open child a concrete correction.
---

Use in the parent issue after inspecting a child deliverable. Start:

```sh
python3 <this-skill-directory>/wrapper.py
```

Follow the returned choices and fill their parameters. `done --child ID` marks the
accepted child done. `resteer --child ID --prompt TEXT` sends a correction.
Stop at either `finished` or `resteer`.
