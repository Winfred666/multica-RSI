---
name: create-symbolic-sop
description: Create or revise a working symbolic SoP by copying the runtime and writing its controller; validate it and inspect its debug preview.
---

Copy [assets/template/](assets/template/) into the new skill directory. Keep
`sop.py`, `wrapper.py` and `monitor.py` unchanged; replace the example
`controller.py` with the procedure. Keep the copied `config.json`; set `DEBUG`
to `true` when you want the compiled HTML preview.

Name Multica SoPs `multica-<name>` using lowercase letters, numbers and hyphens.
Write a short `SKILL.md`: when to use it, required inputs,
`python3 <skill-directory>/wrapper.py`, and how to follow returned choices and
error guidance. Follow recalls in the same turn; reconcile writes before retrying
and use a returned `repaired` command once for a failed assertion.

Export `workflow = Workflow(...)` from `controller.py`. Declare one initial
state and at least one final state; final states have no options. Each option
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
Let the runtime manage status and recovery. Store only small procedure-specific
settings in issue metadata when needed; keep delivery/review records in comments.
When revising a procedure, preserve its checks, side effects, completion rules
and live state.

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
