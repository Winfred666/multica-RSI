---
name: multica-subissue-review
description: Review child replies, resolve blockers and accept verified work in the parent issue.
---

Use in the parent issue after native child steering. Run
`python3 <this-skill-directory>/wrapper.py` and follow its choices.
Read the child's delivery reply and evidence. Accept only delivered work in
`in_review`; code acceptance requires its reviewed commit and an integration
verification command. Approval steers the child to merge that commit into clean
`main`, verify and push remote `main`, then report the integration result through
`multica-surface-delivery`. Settle only after checking that result. The parent then
steers every working sibling with the same Frontend, Backend, CLI or Harness label
to minimally rebase onto new `main`, resolve mechanical conflicts, and block on
true feature conflicts before continuing. Never rewrite sibling worktrees here.
Scan recent thread roots and expand their bounded tails so folded delivery replies
remain visible.
Committed feature branches remain available as evidence.

Resolve blockers in parent scope, return a concrete correction to the same child,
or cancel an unpromising path. For explicit experiment groups, compare all replies
against their common baseline and select the best measured improvement or keep
the baseline. Wait for incomplete groups through native steering.

Finish this SOP once every child is reviewed and settled, then continue the parent
goal. Follow returned error guidance, reconcile writes before retrying, and use
`repaired` once for assertion failures; report repeated failure through
`multica-surface-block`.
