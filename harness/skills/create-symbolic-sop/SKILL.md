---
name: create-symbolic-sop
description: Create or revise executable SOPs with state objects, option actions and optional live prechecks.
---

Copy [assets/template/](assets/template/) and adapt its parallel-refinement procedure.
Skill names may contain only lowercase letters, numbers and hyphens.
Name Multica SOPs with the `multica-` prefix. Keep their `SKILL.md` limited to when
to use the skill, required inputs, the entry command and how to follow its choices.
Implementation details belong in code.
Each invocation stays within one agent task and issue. For another issue or agent,
use native steering or a mention asking its model to call the named SOP. Exchange
results through reply comments and steering prompts; keep delivery and review
records out of issue metadata.
For in-platform capabilities, give rough direction to follow Multica best practice
and the relevant native SOP. Leave platform actions and mechanics to that SOP.

For shared protocol changes, update this skill and its template first and submit
the concrete changes for human review. Only after explicit human approval,
mechanically copy the approved protocol to all symbolic SoPs in `harness/` and
distributions such as `dist/AIDE^2-harness`, preserving controllers and live state.

The standalone package contains `SKILL.md`, `DEBUG.md`, `config.json`, `controller.py`, `sop.py`, `wrapper.py`, `monitor.py`
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
Each wrapper invocation builds a new context. `context['state']` is the task's
JSON record; the template stores its bound test script there. Persist procedure
facts when an action stays in the same state; keep the runtime's `status` and
`recovery` fields unchanged. Read external facts from Multica or Git on later calls.

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
indexes other platform references. RSI ships those documents in
[the platform skill](../multica-platform/SKILL.md).
Use terminal commands `multica issue --help`, `multica issue get --help` and
`multica issue comment add --help` for the installed CLI's exact arguments. Run
help in the terminal, since `cli` expects JSON. Inspect relevant read-only JSON
output for response fields. Other command groups such as `multica agent` are
outside this helper's `issue` prefix; read their help before writing a helper.

Return optional `data`, an `instruction`, or `accepted: False` to stay in the current
state. An action may return `phase` to select a declared state through a scripted
branch; its target precheck still runs. Agent JSON has `status`, a common state
`instruction`, and `next_step`: a list of options, each with its own `instruction`
and executable `command`. Keep state guidance separate from option prompts.
Start with a bare call or `init`, then pass an option name as
the first argument. The wrapper reads the task JSON record and checks its status
before running an option. Invalid options or parameters return current guidance
without changing state. Bare calls run no action. Completion removes the task JSON
file; a later bare call or `init` starts another run within the same native task.
`--help` is read-only: replay the last emitted decision JSON for a recorded task,
including its output and options; for a new or completed task, print the `SKILL.md`
body after YAML frontmatter without creating a task record. Cache each emitted
decision in the existing task record, including callbacks that stay in one state.

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

For an unmet workflow prerequisite, raise `context['error'](code, message, hint=...)`
with the required next action; the saved state stays unchanged. Document the common
exception protocol once in every generated `SKILL.md`, applying to all states:
follow the returned error guidance, reconcile writes before retrying, use `repaired`
once for assertion failures, and report a repeated failure through native Multica block.

Run `python3 <skill-directory>/wrapper.py --validate` and focused checks for affected
paths. Keep repository tests in `tests/`. For conservative refinement, define states directly inside the exported
`Workflow(...)` and keep predefined prechecks with their states. The harness reads
initial and final boundaries from these declarations. Live Multica execution
establishes effectiveness.

Write `DEBUG.md` with a first chart of the interaction states. A scripted branch
between interaction states may use a diamond; branches need not be agent options.
Put state internals in secondary charts. Immediately precede each with
`State: <declared status>`. Show its entry, checks, recalls and exit states so the
chart stands alone; prefer chart labels to prose.

Set `"DEBUG": true` in the skill's `config.json` to publish `<skill-name>-state.html`
on every state shift. The wrapper renders `controller.workflow` states, declared
option targets and the actual transition, with state meanings, native issue/task
IDs and assertion failures. Put the graph before headings and metadata so it is
visible in thumbnails. Selecting a state or edge shows its exact instructions,
parameters and handler source; the view's `--help` shows the same read-only help
as the wrapper. Dynamic routes appear when selected. DEBUG defaults
to `false`. For an existing HTML view, export read-only `artifacts(context)` from
`controller.py` returning `{stable_filename: html_string}`; the wrapper updates
these live artifacts after accepted callbacks. Upload failures leave workflow
results intact.

Use proactive keyword + time-trigger monitoring for long experiment scripts:
the background job keeps logging while the SOP returns `continue`/`exception`.
The launch option must return the monitor state immediately; only its `continue`
recall may block.
The task JSON keeps checkpoint and log positions; the next recall shows only new
output. Follow the recall in the same turn. See the template's [state chart](assets/template/DEBUG.md).
Default to passive native delivery/block steering for Multica sub-issues;
proactive escalation there is reserved for unstable DEV SOPs. A `done` option must
check that every sub-issue, including descendants, is `done`. Otherwise raise a
workflow exception asking the agent to finish reviewing the sub-issues
before recalling `done`; delivery receipts and `in_review` do not pass this gate.
