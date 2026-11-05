---
name: multica-subissue-review
description: Review child deliveries, request corrections or cancellation, and select the best measured experiment result before integration.
---

Use in the parent issue. Start each native task with:

```sh
python3 <this-skill-directory>/wrapper.py
```

Follow the returned review choices exactly, without adding `cd`. Repository binding comes from current or ancestor issue metadata; omit `--repo` after preparation. Read saved delivery answers and `cited_evidence` once. Inspect concrete gaps or changed work; keep measurements and checkpoints. Query advice is a hypothesis until measured in the parent.

Accept only a passed delivery in `in_review`. Query acceptance needs no Git integration or new benchmark. Code acceptance requires the delivered commit and an integration `--verify` command; changed code needs a new report. Handle a blocker in parent scope, then re-steer the same child, or cancel an unpromising path. Children own integration, verification, branch retirement and terminal status.

Only matching explicit `experiment_group` and fixed `experiment_group_size` compete together. While incomplete, correct or cancel members. When all members are delivered or terminal, compare the returned group, select its best measured improvement or `none` when baseline wins, or request corrections. Ordinary experiment children are reviewed individually. A target miss never forces acceptance.

After dispatch, continue useful parent work or prepare authorized parent delivery while settlement runs. Check review once before publication; if children remain active, yield for their native handoff. Failed runs permit correction or cancellation only when no successor is active or pending. Retry a saved settlement only when the failed run identifies its saved wakeup.

Use `parent_context` for verified parent evidence and `parent_delivery` for its delivery instructions and entry command. Prefix **every delivery callback** with the returned `callback_prefix` (`cd <bound-workdir> &&`); review callbacks run as returned. At the delivery report phase, write its JSON once to an absolute `/tmp` path and run the publishing callback in the same tool call. Follow its actual schema and preserve all verification and commit gates.

Inspect successful parent publication, then run `data.completion_command` in a separate tool call. Review ends only when the parent is terminal; continue an unmet parent goal instead of closing it merely to exit review. A nested delivery completion does not complete this review.
