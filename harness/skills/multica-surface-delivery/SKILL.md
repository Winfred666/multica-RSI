---
name: multica-surface-delivery
description: Deliver verified issue work in one native reply with its effect, explanation, results and attachments, then hand off for review.
---

Run `python3 <installed-skill-directory>/wrapper.py` in the current issue's workdir. Follow its `instruction` and `command`. Identity and reply targets come from the native run; supply `--thread ID` only when the script requests the prompt's missing target. Declare any files with `--attachment PATH`.

Confirm only verified requirements; otherwise continue work or use `multica-surface-block` for an external blocker. Follow the commit/report callbacks. Children enter review and notify their parent; the parent owns acceptance. The report is posted as a reply; native steering brings it to the parent. End without another reply.

Lead with the effect, explain the change briefly, then include supporting results and relevant limits in that same reply. Attach supporting files there and finish with relevant code/artifact links. The callback's JSON is input for the native reply, not a separate GitHub evidence report.

Follow returned error guidance, reconcile writes before retrying, and use `repaired` once for assertion failures; report repeated failure through `multica-surface-block`.
