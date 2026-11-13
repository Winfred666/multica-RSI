# Symbolic SoP developer kit

The CLI supplies the Python engine and Workflow API. A skill supplies its
instructions and controller. Python 3 is required; no engine package install
or copied wrapper is needed.

## Start

```sh
multica sop dev init .agents/skills/my-sop --minimal
# Or: multica sop dev init .agents/skills/my-sop --feature-rich
multica sop dev validate .agents/skills/my-sop
multica sop run my-sop
```

`init` accepts an empty or nonexistent directory. The directory's name becomes
the skill name (lowercase, with punctuation replaced by hyphens). Minimal is
the default. Feature-rich adds file inspection, parameters, branching and review.
Both include this searchable `DEVELOPMENT.md`; `multica sop dev docs` prints it.

## Skill files

`SKILL.md` declares the name and selector, then gives procedure-specific guidance:

```markdown
---
name: my-sop
metadata: {sop: symbolic}
---
Run `multica sop run my-sop` and follow its returned choices.
```

`controller.py` exports a Workflow. The engine makes `sop`, `wrapper`, `common`
and `monitor` available to its imports:

```python
from sop import FINISHED, Option, State, Workflow

workflow = Workflow(
    State('ready', 'Choose finish.', initial=True, options=(
        Option('finish', FINISHED, 'Complete the procedure'),
    )),
)
```

## States, options and completion

- `State(name, description, initial=True, options=(...))` declares the one initial
  state. State names use lowercase letters, numbers and underscores; start with
  a letter. `init`, `repaired`, `error` and `blocked` are runtime names.
- `Option(name, target, prompt, action=None, params=(...))` offers a callback.
  Its target is a state, a Branch, or `FINISHED`. Actions receive `(args, context)`.
- `Parameter('--path', 'File to inspect')` declares an option input. Parameters
  are required by default; use `required=False` for optional inputs. Argument
  parser options such as `type=Path` and `action='store_true'` are supported.
- `Branch(name, description, {'label': 'state', ...})` declares automatic routes.
  Its action returns `{'phase': 'state'}` using a declared destination, not a label.
- `FINISHED` completes the procedure and clears its issue metadata key. A named
  `State(..., final=True)` also completes it and has no options.

## Controller hooks and context

`view(context)` returns the state's current `instruction` and optional internal
`data`. `precheck(context)` checks live prerequisites before/after an action;
an assertion failure enters the engine's repair flow. States can declare these
hooks and an `owner={'id': agent_id, 'name': agent_name}` (or an owner callable).
`Workflow(..., handoff=callback)` supports scripted ownership handoffs.

`context['issue_id']` and `context['workspace_id']` identify execution. Mutable
`context['state']` is stored in the issue's `sop.<name>` metadata key. Return
`{'instruction': '...'}` for guidance, `{'phase': 'destination'}` for a branch,
or `{'accepted': False, 'instruction': '...'}` to stay in the current state.
`context['cli'](*words, write=False)` calls `multica issue`; mark writes with
`write=True`. Use `context['error'](code, message)` for an actionable error.

The process keeps the caller's cwd. Use `context['sop_dir']` or
`Path(__file__).parent` for skill assets and `context['state_dir']` for local
execution files. Keep skill names at most 60 characters.

## Help, validation and preview

`multica sop dev validate DIRECTORY` checks metadata, Python imports and the
exported Workflow definition. It works directly on a directory, without an
issue or task context.

`multica sop run NAME --help` shows the skill body before starting, or replays
the current saved guidance. Authoring docs are only under `sop dev`.

Config is optional and DEBUG is off by default. To enable compiled HTML previews,
add `config.json` containing `{"DEBUG": true}`. The engine derives the state
graph from Workflow; action docstrings may add local logic in fenced `dot`
blocks, as shown in the feature-rich controller. The wrapper's `ensure_command`
helper supplies Graphviz for compilation. Config and previews are published as
ownerless issue soft-links; `controller.artifacts(context)` can return ordinary
comment attachments as a filename-to-content mapping.
