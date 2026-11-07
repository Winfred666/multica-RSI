---
name: multica-surface-delivery
description: Deliver verified work with its effect and short primary evidence in a native reply or artifact, then hand off for review.
---

Run `python3 <installed-skill-directory>/wrapper.py` in the current issue's workdir. Follow its `instruction` and `command`. Identity and reply targets come from the native run; supply `--thread ID` only when the script requests the prompt's missing target. Declare any files with `--attachment PATH`.

Confirm only verified requirements; otherwise continue work or use `multica-surface-block` for an external blocker. Follow the commit/report callbacks. Children enter review and notify their parent; the parent owns acceptance. The report is posted as a reply; native steering brings it to the parent. End without another reply.

Lead with the effect and a brief explanation. Put strong, short primary evidence in the native reply or a native artifact; link longer secondary evidence by URL or path. Never put Multica evidence on GitHub or in the tracked code repository. The callback's JSON is temporary input for the reply.

Follow returned error guidance, reconcile writes before retrying, and use `repaired` once for assertion failures; report repeated failure through `multica-surface-block`.
