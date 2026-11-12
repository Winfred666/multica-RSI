# Symbolic SoP protocol debugging

Read this when diagnosing runtime, recovery or HTML output, or changing the
shared protocol. Ordinary SoP authoring uses [SKILL.md](SKILL.md) and the copied
runtime unchanged. This creator is a textual skill; this file is its maintenance
reference, not a duplicate state graph for a generated SoP.

## Shared protocol changes

Update this skill and its template first and submit the concrete changes for
human review. Only after explicit approval, mechanically copy the approved
protocol to symbolic SoPs in `harness/` and distributions such as
`dist/AIDE^2-harness`, preserving controllers and live state.

The standalone package includes `SKILL.md`, `config.json`, `controller.py`,
`sop.py`, `wrapper.py`, `common.py`, `monitor.py` and procedure dependencies. The runtime owns one serialized current record in
`issue.metadata["sop.<skill-name>"]`; preserve it during updates. `config.json`
contains a boolean `DEBUG` (default `false`). Changing issue metadata does not
configure that package flag.

## Controller API details

`Parameter('--flag', 'What to supply')` declares required input. Use
`required=False` for optional inputs and `value=known_value` to render and enforce
known values. Argparse arguments such as `type=Path`, `choices=...` and
`action='store_true'` pass through. Shared optional inputs go in `Workflow(params=...)`.

Actions receive `(args, context)`; prechecks, views and option factories receive
`context`. Read the object interfaces in [sop.py](assets/template/sop.py) and the
context construction and `cli` implementation in [wrapper.py](assets/template/wrapper.py).

| Context key | Value and availability |
| --- | --- |
| `issue_id`, `workspace_id` | Native issue/workspace IDs. A declared `--issue` input can override the issue; `--workspace` must match the native workspace. |
| `agent_id` | Native `MULTICA_AGENT_ID`, or `None` when absent. |
| `phase` | State name used for option selection or guidance. In a target precheck, use `result['phase']` for the target. |
| `args` | Argparse namespace. Shared workflow inputs are available before the current precheck; selected option inputs are parsed before its action. `--some-flag` becomes `args.some_flag`; `args.event` is the selected option name, or `init` on a bare call. |
| `initial` | Current precheck's return value, available after that check; `None` if it has no precheck. |
| `result` | Target precheck only: generated transition result, including `phase` and any `data`, `instruction` or `accepted` value. Use `'result' in context` to distinguish target checks from current checks. |
| `cli` | Callable `cli(*words, write=False)` described below. |
| `save_state` | Call after changing procedure facts in `context['state']`. |
| `error` | Exception class: `raise context['error']('invalid_child', 'Choose an open direct child.', hint=None)`. |

Use your own context keys to cache JSON-compatible observations within one call.
Each invocation reads fresh issue metadata. `context['state']` is the SoP's
current record; the template stores its bound test script there. Persist procedure
facts with `context['save_state']()` when needed; keep the runtime's `status` and
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

Keep `data` internal. Export only facts needed for the next choice through state
instructions or option prompts. CLI, help and HTML share the same filtered decision
schema. Return optional `data`, an `instruction`, or `accepted: False` to stay in the current
state. For scripted state selection, target a declared `Branch` and return `phase`
to select one of its routes; the target state's precheck still runs. Agent JSON has `status`, a common state
`instruction`, and `next_step`: a list of options, each with its own `instruction`
and executable `command`. Keep state guidance separate from option prompts.
Start with a bare call or `init`, then pass an option name as
the first argument. The wrapper reads the issue's current SoP status before an
option runs. Invalid options return current guidance without changing state.
Bare calls run no action. Completion and quit delete only this SoP's key.
`--help` reads the latest saved decision without executing the controller or
writing metadata. Without a record it shows the skill body. Handoff rebinds saved
commands to the receiving checkout. Keep only current status, latest guidance
and necessary procedure facts; do not save decision history, diagram caches or
local recovery/outbox files. The existing 8 KB limit covers all issue metadata;
coordinate callbacks on the same issue sequentially.

Use `view(context)` for decision evidence and `options(context)` for choices bound
to actual IDs. Keep both read-only; reuse observations in the current context.
Option factories can run before selection, so option-specific arguments may not
exist yet. Return JSON-compatible precheck observations and action results so the
wrapper can save them for recovery. The refinement template runs tests and writes logs in the current issue folder;
it stores no workspace folder configuration. On another machine, make the test
script, solution and existing job files available before continuing.
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

For conservative refinement, define states directly inside the exported
`Workflow(...)` and keep predefined prechecks with their states. The harness reads
initial and final boundaries from these declarations.

Shared configuration JSON belongs in artifacts through `common.write_config` and
`read_config`; keep only the existing small references in metadata. See
[configuration artifacts](references/config-artifacts.md). Runtime status/recovery
stays runtime-owned; delivery/review records belong in comments. Use native
steering or mentions for another issue or agent.

## Workflow graph and local action diagrams

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
after its function. There is no overview block in a docstring. Generated SoPs use the
compiled HTML rather than a separately maintained `DEBUG.md` graph. Keep the action diagram short and consistent with its implementation.

Use Graphviz's `point` start marker and a filled ringed-circle stop.
The start boundary is diagram-only. R-328's `FINISHED` sentinel owns the stop
marker: selecting it shows completion/re-entry guidance without calling workflow
code. Options and Branch routes may target `FINISHED` directly. A declared final
state with no options is also supported. State owners appear in the graph;
scripted handoffs and guarded `--no-start` assignee updates remain runtime work.

Set `"DEBUG": true` in the skill's `config.json` to publish `<skill-name>-state.html`
on every state shift as an ordinary comment attachment, following R-328's
`common.write_artifact` delivery. Shared JSON configuration, not HTML previews,
is the artifact-first target; see [configuration artifacts](references/config-artifacts.md).
The wrapper renders `controller.workflow` states, option
targets and symbolic branches, with state meanings, native issue
ID and assertion failures. Put the graph before headings and metadata so it is
visible in thumbnails. Use a CSS orientation media query: in landscape, fit SVGs
to the available width without horizontal scrolling; in portrait, keep their natural
size and allow horizontal scrolling. This follows the HTML viewport and updates
automatically on resize or rotation. Selecting a state shows formatted agent JSON. The current state replays the
latest guidance; other states show declared guidance and static options without
executing workflow code. Selecting an edge shows the agent's
option JSON followed by its rendered DOT logic, before issue metadata.
State selection shows the diagrams for its available options. Read docstrings with
`inspect.getdoc`; omit function definitions from the preview. Use choices resolved in the current invocation for dynamic option diagrams;
never run option factories solely to render a preview.
The wrapper automatically installs Graphviz on the CLI/daemon host when `dot`
is missing, using the same `ensure_command` helper as bubblewrap consumers.
Compile the global DOT graph and local DOT diagrams with `dot -Tsvg` before uploading
the HTML. Use Graphviz's native attributes for simple styling and horizontal `rankdir=LR` layout;
let `dot` place nodes and route edges. Avoid custom ranking, label wrapping or SVG minification.
Use native edge `label` so layout reserves space for option text. Let nodes and
diamonds grow to contain their full text with normal padding. Keep borders plain.
Limit CSS to readable text, arrow-label contrast and viewport scrolling; retain
browser focus indicators and omit decorative effects.
Embed each local SVG once in an HTML template, referenced by state and option selections,
using small local JavaScript; opening or
clicking the artifact must require no external scripts, fonts, network or browser-side
layout engine. Keep missing-tool and invalid-DOT errors actionable; artifact failures
must leave workflow results intact.
Omit HTML help controls.
Resolve dynamic option targets during normal choice resolution. DEBUG defaults
to `false`. For an existing HTML view, export read-only `artifacts(context)` from
`controller.py` returning `{stable_filename: html_string}`; the wrapper updates
these ordinary comment artifacts after accepted callbacks, like DEBUG previews.
Publication failures leave workflow results intact and report warnings on stderr.


## External tools

At an existing dependency call site, use `from wrapper import ensure_command`,
then `ensure_command('dot', 'graphviz')` or
`ensure_command('bwrap', 'bubblewrap')`. The helper reuses PATH, otherwise installs
the declared package with the host package manager and checks PATH again.
Installation errors return `dependency_install_failed`; resolve the installer
error before retrying. Keep dependency checks out of read-only help. This helper
installs tools; it does not enforce sandboxing around child launches.

## Long-running procedures

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

## Checking a suspected failure

Run `python3 <creator-directory>/scripts/check.py <skill-directory>` first.
The checker validates the package and runs shared-runtime checks in temporary
template fixtures using the package's `sop.py`, `wrapper.py` and `monitor.py`;
it does not execute domain actions or replace live state. Omit the directory to
check the canonical template. Verify the affected procedure separately and keep
the checker current when changing protocol behavior.
