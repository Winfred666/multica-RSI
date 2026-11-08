---
name: multica-subissue-review
description: Review child replies, resolve blockers and accept verified work in the parent issue.
---

Use in the parent issue after native child steering. Run
`python3 <this-skill-directory>/wrapper.py` and follow its choices.
Read the child's delivery reply and evidence. Accept only delivered work in
`in_review`; code acceptance requires its reviewed commit and an integration
verification command. The parent performs integration before marking the child done.
Committed feature branches remain available as evidence.

Resolve blockers in parent scope, return a concrete correction to the same child,
or cancel an unpromising path. For explicit experiment groups, compare all replies
against their common baseline and select the best measured improvement or keep
the baseline. Wait for incomplete groups through native steering.

Finish this SOP once every child is reviewed and settled, then continue the parent
goal. Follow returned error guidance, reconcile writes before retrying, and use
`repaired` once for assertion failures; report repeated failure through
`multica-surface-block`.

See [DEBUG.md](DEBUG.md) for this procedure’s states. In every state, follow
returned error guidance and finish unmet prerequisites before retrying. Reconcile
writes first. For assertion failures, use the returned `repaired` callback once;
report a repeated failure through native Multica block.
