---
name: multica-surface-delivery
description: Deliver verified issue work with a self-check, concise evidence report and the correct review or done handoff.
---

Run `python3 <installed-skill-directory>/wrapper.py` in the current issue's workdir. Follow its `instruction` and `command`. Identity and reply targets come from the native run; supply `--thread ID` only when the script requests the prompt's missing target. Declare any files with `--attachment PATH`.

Confirm only verified requirements; otherwise continue work or use `multica-surface-block` for an external blocker. Follow the commit/report callbacks. Children enter review and notify their parent; the parent owns acceptance. The script closes threads and hands off; end without another reply.
