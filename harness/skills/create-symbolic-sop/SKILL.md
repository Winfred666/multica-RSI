---
name: create-symbolic-sop
description: Create or revise a working symbolic SoP by copying the runtime and writing its controller; validate it and inspect its debug preview.
---

Copy [assets/template/](assets/template/) into the new skill directory. Keep
`sop.py`, `wrapper.py`, `common.py` and `monitor.py` unchanged; replace the example
`controller.py` with the procedure. Keep the copied `config.json`; set `DEBUG`
to `true` when you want the compiled HTML preview.

Name Multica SoPs `multica-<name>` using lowercase letters, numbers and hyphens;
keep names at most 60 characters.
Write a short `SKILL.md`: when to use it, required inputs,
`python3 <skill-directory>/wrapper.py`, and how to follow returned choices and
error guidance. Follow recalls in the same turn; reconcile writes before retrying
and use a returned `repaired` command once for a failed assertion.

Export `workflow = Workflow(...)` from `controller.py`. Declare one initial
state. Complete through `FINISHED` (imported from `sop`) or a final state with no options. Each option
names its target, prompt, action and inputs. Script fixed work; leave judgment
at explicit choices. For example:

```python
from pathlib import Path
from sop import Option, Parameter, State, Workflow


def verify(args, context):
    if not Path(args.path).is_file():
        raise context['error']('missing_file', 'Create the file, then retry.')


workflow = Workflow(
    State('inspect', 'Choose the file to verify.', initial=True, options=(
        Option('verify', 'finished', 'Verify the file exists.', verify,
               params=(Parameter('--path', 'File to verify'),)),
    )),
    State('finished', 'File verified; stop.', final=True),
)
```

Actions receive `(args, context)`. Use read-only `precheck(context)` for live
prerequisites and `view(context)` for decision evidence. `context['issue_id']`
identifies the current issue; `context['cli'](*words, write=False)` runs
`multica issue <words> --output json`. Pass separate arguments and mark writes
with `write=True`; use native Multica SoPs for platform workflows.
The runtime stores status and recovery in `issue.metadata["sop.<skill-name>"]`.
Keep controller `data` internal; expose needed facts through instructions/options.
Transient execution/recovery state stays in that metadata key. Shared configuration
payloads belong in JSON attachments, not issue metadata; retain only small references
for existing readers. Follow [configuration artifacts](references/config-artifacts.md)
for the master/delegation pattern and canonical attachment identity.

The shared runtime follows [R-328's accepted protocol](https://github.com/Winfred666/multica-featssship/tree/c396c654f245e2dd4d0686f70a16a73101ac4188/installation).
A state may declare `owner={"id": ..., "name": ...}` (or a callable). The runtime
checks that owner before actions, uses `Workflow(handoff=...)` for the existing
scripted handoff, and synchronizes accepted destination ownership with
`issue assign --to-id ... --no-start`. Help and rejected transitions do not hand off.
`FINISHED` clears transient state and returns re-entry guidance.
When revising a procedure, preserve its checks, side effects, completion rules
and live state. Keep delivery/review records in comments.

HTML debug/controller artifacts use ordinary comment attachments through
`common.write_artifact`; do not turn them into ownerless configuration resources.
Change this creator/template first; obtain human approval before propagating to
other symbolic SoPs or installed distributions.

The preview derives the state graph from `Workflow`; use `Branch` for automatic
script-selected transitions. Optional action docstrings can explain local logic.
For API details, protocol debugging or preview tuning, read [DEBUG.md](DEBUG.md)
only as needed. Creating a SoP does not require changing the runtime or renderer.

Run `python3 <creator-directory>/scripts/check.py <skill-directory>`, then exercise
the procedure in a disposable fixture. Start with `python3 <skill-directory>/wrapper.py`
and follow the returned commands in the native Multica run environment.
`--help` replays saved guidance without executing the procedure; before starting
or after completion it shows the skill body. Inspect the compiled HTML when debug
mode is enabled.
