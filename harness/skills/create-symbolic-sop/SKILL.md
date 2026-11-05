---
name: create-symbolic-sop
description: Create or revise executable SOPs with state objects, option actions and optional live prechecks.
---

Copy [assets/template/](assets/template/) and replace its child-review procedure.
Name Multica SOPs with the `multica-` prefix. Keep their `SKILL.md` limited to when
to use the skill, required inputs, the entry command and how to follow its choices.
Implementation details belong in code.

The standalone package contains `SKILL.md`, `controller.py`, `sop.py`, `wrapper.py`
and procedure dependencies. The wrapper creates `.state/<task_id>.json` at runtime.
Preserve live state when revising an installed skill. Customize `controller.py`;
`sop.py` defines the objects and `wrapper.py` drives them.

Export `workflow = Workflow(...)` with ordinary `State` objects. Declare exactly
one initial state and one or more final states with `final=True`. Each state has a brief
description, options, and optional read-only `precheck(context)` and `view(context)`.

Each `Option(name, target, prompt, action, params=...)` owns its action. Use a short
named function; avoid subclasses, entry/exit actions and event dispatch chains.
`Parameter('--flag', 'What to supply')` declares required input. Use
`required=False` for optional inputs and `value=known_value` to render and enforce
known values. Argparse arguments such as `type=Path`, `choices=...` and
`action='store_true'` pass through. Shared optional inputs go in `Workflow(params=...)`.

```python
from sop import Option, Parameter, State, Workflow


def record(args, context):
    context['cli']('comment', 'add', context['issue_id'],
                   '--content', args.finding, write=True)


workflow = Workflow(
    State('inspect', 'Inspect the evidence.', initial=True, options=(
        Option('record', 'finished', 'Record the confirmed finding.', record,
               params=(Parameter('--finding', 'Confirmed finding'),)),
    )),
    State('finished', 'Finding recorded; stop.', final=True),
)
```

Actions receive `(args, context)`; prechecks, views and option factories receive
`context`. Read the object interfaces in [sop.py](assets/template/sop.py) and the
context construction and `cli` implementation in [wrapper.py](assets/template/wrapper.py).

| Context key | Value and availability |
| --- | --- |
| `task_id` | Native `MULTICA_TASK_ID` string; identifies this run's JSON record. |
| `issue_id`, `workspace_id` | Native issue/workspace IDs. A declared `--issue` input can override the issue; `--workspace` must match the native workspace. |
| `agent_id` | Native `MULTICA_AGENT_ID`, or `None` when absent. |
| `phase` | State name used for option selection or guidance. In a target precheck, use `result['phase']` for the target. |
| `args` | Argparse namespace. Shared workflow inputs are available before the current precheck; selected option inputs are parsed before its action. `--some-flag` becomes `args.some_flag`; `args.event` is the selected option name, or `init` on a bare call. |
| `initial` | Current precheck's return value, available after that check; `None` if it has no precheck. |
| `result` | Target precheck only: generated transition result, including `phase` and any `data`, `instruction` or `accepted` value. Use `'result' in context` to distinguish target checks from current checks. |
| `cli` | Callable `cli(*words, write=False)` described below. |
| `error` | Exception class: `raise context['error']('invalid_child', 'Choose an open direct child.', hint=None)`. |

Use your own context keys to cache JSON-compatible observations within one call.
Each wrapper invocation builds a new context; read durable procedure facts from
Multica or Git on later calls. Keep runtime-managed keys unchanged.

`cli` runs `multica issue <words> --output json`, inheriting the process environment
and working directory, with a 30-second timeout. Pass separate string arguments, without `multica`,
`issue`, shell quoting or `--output`. It returns parsed JSON (the command's dict or
list) and raises `context['error']` with code `cli_error` on a nonzero exit.
Timeouts and invalid JSON also raise exceptions. Mark mutations with `write=True`;
this records attempted writes for recovery, without retrying or changing permissions.

```python
issue = context['cli']('get', context['issue_id'])
grouped = context['cli']('children', issue['id'])  # unstaged + stages[].issues
context['cli']('comment', 'add', issue['id'], '--content', args.finding, write=True)
```

Read `multica-platform/references/issues.md` in the installed skills directory for
issue fields, status side effects, child issues and comment contracts; its `SKILL.md`
indexes other platform references. Use terminal commands `multica issue --help`, `multica issue get --help` and
`multica issue comment add --help` for the installed CLI's exact arguments. Run
help in the terminal, since `cli` expects JSON. Inspect relevant read-only JSON
output for response fields. Other command groups such as `multica agent` are
outside this helper's `issue` prefix; read their help before writing a helper.

Return optional `data`, an `instruction`, or `accepted: False` to stay in the current
state. The wrapper renders numbered choices, target descriptions, parameters and
executable commands. Start with a bare call or `init`, then pass an option name as
the first argument. The wrapper reads the task JSON record and checks its status
before running an option. Invalid options or parameters return current guidance
without changing state. Bare calls run no action. Completion removes the task JSON
file; a later bare call or `init` starts another run within the same native task.

Use `view(context)` for decision evidence and `options(context)` for choices bound
to actual IDs. Keep both read-only; reuse observations in the current context.
Option factories can run before selection, so option-specific arguments may not
exist yet. Return JSON-compatible precheck observations and action results so the
wrapper can save them for recovery.
Script fixed work and leave judgment at explicit options. Preserve the procedure's
conditions, side effects, rejection paths and termination behavior.

A precheck reads live state and explicitly raises `AssertionError` on failure,
including under Python `-O`. The current check runs before the action; the target
check verifies its effects, including completion. Its context includes `args`,
`initial` (the current check's return value) and `result`. Preserve existing checks
and their state bindings. `Completed(instruction, data)` may signal already-finished
work; the target check still runs. Supply `target=STATE` for a final state other
than the default `finished`.

Failed checks return a repair command. After correcting the condition and
reconciling external writes, run it once. A repaired target check commits the saved
result without repeating the action. A second failure returns `blocked` to the
caller; bare resume does not retry.

Run `python3 <skill-directory>/wrapper.py --validate` and focused checks for affected
paths. Keep repository tests in `tests/`. Define states directly inside the exported `Workflow(...)` and preserve their
prechecks when revising a procedure. Live execution establishes effectiveness.
