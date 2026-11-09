---
name: create-symbolic-sop
description: Create or revise executable SOPs with state objects, option actions and optional live prechecks.
---

Copy [assets/template/](assets/template/) and adapt its parallel-refinement procedure.
Skill names may contain only lowercase letters, numbers and hyphens.
Name Multica SOPs with the `multica-` prefix. Keep their `SKILL.md` limited to when
to use the skill, required inputs, the entry command and how to follow its choices.
Implementation details belong in code.
Each SoP belongs to an issue, shared across agent tasks and machines. Hand off
through native steering or a mention asking the next agent to call the same SoP
on the same issue. Store runtime state in its own `sop.<skill-name>` metadata key;
exchange delivery and review evidence through native issue comments.
For in-platform capabilities, give rough direction to follow Multica best practice
and the relevant native SOP. Leave platform actions and mechanics to that SOP.

For shared protocol changes, update this skill and its template first and submit
the concrete changes for human review. Only after explicit human approval,
mechanically copy the approved protocol to all symbolic SoPs in `harness/` and
distributions such as `dist/AIDE^2-harness`, preserving controllers and live state.

The standalone package contains `SKILL.md`, `config.json`, `controller.py`, `sop.py`, `wrapper.py`, `monitor.py`
and procedure dependencies. The wrapper stores serialized JSON in the issue metadata
key `sop.<skill-name>` using the existing scalar metadata API. Names must be at most
60 characters to fit the key. No local task state file or fallback is used. Preserve
issue metadata when revising an installed skill. Customize `controller.py`;
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
| `issue_id`, `workspace_id` | Required native issue/workspace IDs. A declared `--issue` input can override the issue; `--workspace` must match the native workspace. |
| `agent_id` | Native `MULTICA_AGENT_ID`, or `None` when absent. |
| `phase` | State name used for option selection or guidance. In a target precheck, use `result['phase']` for the target. |
| `args` | Argparse namespace. Shared workflow inputs are available before the current precheck; selected option inputs are parsed before its action. `--some-flag` becomes `args.some_flag`; `args.event` is the selected option name, or `init` on a bare call. |
| `initial` | Current precheck's return value, available after that check; `None` if it has no precheck. |
| `result` | Target precheck only: generated transition result, including `phase` and any `data`, `instruction` or `accepted` value. Use `'result' in context` to distinguish target checks from current checks. |
| `cli` | Callable `cli(*words, write=False)` described below. |
| `save_state` | Call `context['save_state']()` after updating procedure facts in `context['state']`. Available to checks and actions. |
| `error` | Exception class: `raise context['error']('invalid_child', 'Choose an open direct child.', hint=None)`. |

Use your own context keys to cache JSON-compatible observations within one call.
Each wrapper invocation builds a new context. `context['state']` is the issue's
SoP metadata record; the template stores its bound test script there. Persist procedure
facts with `context['save_state']()` when an action stays in the same state; keep the runtime's `status` and
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
state. For scripted state selection, target a declared `Branch` and return `phase`
to select one of its routes; the target state's precheck still runs. Agent JSON has `status`, a common state
`instruction`, and `next_step`: a list of options, each with its own `instruction`
and executable `command`. Keep state guidance separate from option prompts.
Start with a bare call or `init`, then pass an option name as
the first argument. The wrapper reads the issue's SoP metadata and checks its status
before running an option. Invalid options or parameters return current guidance
without changing state. Bare calls run no action. Completion or initial-state `quit`
deletes only this SoP's metadata key; another call can start again on the same issue.
`--help` is read-only: replay the last emitted decision JSON for this issue and SoP,
including its output and options; for a new or completed SoP, print the `SKILL.md`
body after YAML frontmatter without writing metadata. Keep one current status record
per SoP, including only its latest decision and required procedure facts. Replace that
record on each recall; do not accumulate decision history or persist diagram caches.
Status and recovery use only this issue metadata record. Do not read, write or
fall back to local status files, and do not include native run identifiers.
On another checkout, replayed commands use the receiving Python and wrapper path;
help imports no controller and runs no checks or actions. State access errors must
fail visibly, never start a new local run. Metadata writes replace only this key,
preserving other SoPs and unrelated keys. Callbacks on one SoP must be coordinated
sequentially; the API has no compare-and-set for overlapping callbacks. The existing
8 KB limit covers all metadata on the issue; keep stored facts and guidance compact.
Issue state is portable; workspace files, local jobs and logs still require their
procedure's handoff on another machine.

Use `view(context)` for decision evidence and `options(context)` for choices bound
to actual IDs. Keep both read-only; reuse observations in the current context.
Option factories can run before selection, so option-specific arguments may not
exist yet. Return JSON-compatible precheck observations and action results so the
wrapper can save them for recovery. In the refinement template, the saved solution
`workspace` pins the test's working directory and log root across recalls; keep it
internal rather than repeating it in agent guidance.
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

Run `python3 <creator-directory>/scripts/check.py <skill-directory>` when creating
or revising a SOP. The [checker](scripts/check.py) validates the package, then runs shared-runtime checks
in temporary template fixtures using that package's `sop.py`, `wrapper.py` and
`monitor.py`; it does not execute the procedure's actions or replace live state.
Omit the directory to check the canonical template. Keep these checks current
with protocol changes, and verify procedure-specific behavior separately.
For conservative refinement, define states directly inside the exported
`Workflow(...)` and keep predefined prechecks with their states. The harness reads
initial and final boundaries from these declarations. Live Multica execution
establishes effectiveness.

The global state graph comes only from `Workflow`: its `State` objects, option
targets and explicit `Branch` declarations. An option is an agent choice. A
diamond is an automatic transition selected by script output, with no extra
option or callback. Import `Branch` from `sop` and declare it inside `Workflow`,
for example:

```python
Branch('configure_result', 'Delivery format valid?',
       {'yes': 'variant', 'no': 'scaffold'})
```

Set the configure option's target to `configure_result`. Its action returns
`phase: 'variant'` or `phase: 'scaffold'`; the runtime checks that this phase is
declared in the branch's routes. Branches are transient and route directly to
states. Labels describe the script outcomes. The renderer reads these declarations
without running conditions or inferring routes from docstrings or execution history.

An action's optional fenced `dot` docstring supplies only its
expanded local flowchart. Show validation, exceptions, side effects and local
branches or loops that change the action's work. Keep global state-routing
diamonds and state-to-state links out of these diagrams. Name each DOT `digraph`
after its function. There is no overview block in a docstring and no duplicate
`DEBUG.md`. Keep the action diagram short and consistent with its implementation.

Use Graphviz's `point` start and filled `doublecircle` stop shapes linked to the
initial/final states with plain arrows. These markers and boundary links are not
clickable or workflow options.
Final states have no options; the review action can complete directly without an
extra finish action. Declare its normal completion target so the final state is
connected before execution; handlers can still return another state for a retry.

Set `"DEBUG": true` in the skill's `config.json` to publish `<skill-name>-state.html`
on every state shift. The wrapper renders `controller.workflow` states, option
targets and symbolic branches, with state meanings, the issue ID and SoP name and assertion failures. Put the graph before headings and metadata so it is
visible in thumbnails. Use a CSS orientation media query: in landscape, fit SVGs
to the available width without horizontal scrolling; in portrait, keep their natural
size and allow horizontal scrolling. This follows the HTML viewport and updates
automatically on resize or rotation. Selecting the current state shows its latest
agent JSON; other states show declared guidance and static options without
executing workflow code. Selecting an edge shows the agent's
option JSON followed by its rendered DOT logic, before issue/SoP metadata.
State selection shows the diagrams for its available options. Read docstrings with
`inspect.getdoc`; omit function definitions from the preview. Keep diagrams for
dynamic options only in the current invocation when the workflow resolves them;
do not rerun option factories to render a preview.
Install Graphviz on the CLI/daemon host and expose `dot` on PATH (on Debian/Ubuntu:
`sudo apt-get install --no-install-recommends graphviz`). Like the sandbox helper's
external `bwrap` lookup, the renderer finds the system executable and calls it directly.
Compile the global DOT graph and local DOT diagrams with `dot -Tsvg` before uploading
the HTML. Use shared Graphviz defaults for fonts, rounded states, soft colors and spacing.
Lay out global forward flow downward in Workflow declaration order; return arrows
remain visible but do not constrain ranks. Keep presentation separate from routing.
Embed each local SVG once in an HTML template, referenced by state and option selections,
and omit SVG comments and inter-element whitespace. Use small local JavaScript; opening or
clicking the artifact must require no external scripts, fonts, network or browser-side
layout engine. Keep missing-tool and invalid-DOT errors actionable; artifact failures
must leave workflow results intact.
Omit HTML help controls.
Keep dynamic option targets in the current invocation so previews never repeat
option factories. DEBUG defaults
to `false`. For an existing HTML view, export read-only `artifacts(context)` from
`controller.py` returning `{stable_filename: html_string}`; the wrapper updates
these live artifacts after accepted callbacks. Upload failures leave workflow
results intact.

Use proactive keyword + time-trigger monitoring for long experiment scripts:
the background job keeps logging while the SOP returns `continue`/`exception`.
The launch option must return the monitor state immediately; only its `continue`
recall may block.
The SoP metadata keeps checkpoint and log positions; the next recall shows only new
output. Follow the recall in the same turn.
Default to passive native delivery/block steering for Multica sub-issues;
proactive escalation there is reserved for unstable DEV SOPs. A `done` option must
check that every sub-issue, including descendants, is `done`. Otherwise raise a
workflow exception asking the agent to finish reviewing the sub-issues
before recalling `done`; delivery receipts and `in_review` do not pass this gate.
