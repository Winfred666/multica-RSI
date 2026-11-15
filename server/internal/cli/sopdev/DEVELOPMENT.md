# Symbolic SoP developer kit

Use `multica sop docs` for symbolic SoP creation guidance. The CLI supplies the
Python engine and Workflow API; no separate authoring skill is needed. A skill supplies its
instructions and controller. Python 3 is required; no engine package install
or copied wrapper is needed.

## Start

```sh
multica sop docs
multica sop dev init .agents/skills/my-sop --minimal
# Or: multica sop dev init .agents/skills/my-sop --feature-rich
multica sop dev validate .agents/skills/my-sop
multica sop run my-sop
```

`init` accepts an empty or nonexistent directory. The directory's name becomes
the skill name (lowercase, with punctuation replaced by hyphens). Minimal is
the default. Feature-rich adds file inspection, parameters, branching and review.
Both include this searchable `DEVELOPMENT.md`; `multica sop docs` prints it.
`multica sop dev docs` remains a compatible spelling of the same guide.

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
    State('ready', 'Choose finish.', initial=True),
    Option('finish', prompt='Complete the procedure'),
    edges=(('ready', 'finish'), ('finish', FINISHED)),
)
```

## Pair graph

- Declare each State, Option and Branch once with a unique name. `edges` contains
  only `(source_name, destination_name)` pairs. States pause; Options execute
  `(args, context)` callbacks; Branch selectors return an outgoing destination
  name. A Branch without a selector uses the preceding action's `phase`.
- `State(..., initial=True)` declares the one initial State. `final=True` and
  `FINISHED` complete the procedure and clear its issue metadata key.
- Choices are outgoing State edges. Direct State choices use the destination
  name as the command and its description as the prompt. An optional
  `State(..., options=factory)` returns declared destination names or
  parameter-bound Options for that invocation only; it cannot add routes.
- `Option(name, prompt, action=None, params=())` declares a command and inputs.
  `Parameter('--path', 'File to inspect')` is required unless `required=False`;
  argparse settings such as `type=Path` and `action='store_true'` are supported.
- Every automatic branch must reach a State or `FINISHED`. Validation reports
  action-only cycles/dead ends by path; State retry loops remain valid. Validation
  and previews never execute callbacks or factories.
- Consecutive Options share the configured `sop_max_action_depth` budget
  (default five); reaching a State resets it. Branches do not consume depth.
  Actions must follow declared edges. Chains are not transactions or retries.

## Controller hooks and context

`view(context)` returns the state's current `instruction` and optional internal
`data`. `precheck(context)` checks live prerequisites before/after an action;
an assertion failure enters the engine's repair flow. States can declare these
hooks and an `owner={'id': agent_id, 'name': agent_name}` (or an owner callable).
`Workflow(..., handoff=callback)` supports scripted ownership handoffs.

Addressed questionnaire JSON includes frontend-ignored `how_to_step` guidance for
posting `/note !sop-remote NAME {"que_comment_id":"<this-comment-id>","args":[...]}`
as an ordinary comment. Use the rendered questionnaire comment's ID and the chosen
command's argument strings after `multica sop run NAME`, including the rendered
`--issue` source binding. Cross-issue questions forward an HTTP(S) Markdown source
link and owner `content` to the declared thread, then store `que_comment_id` and
`forwarded_comment_id` in the source JSON. Same-issue questions do not forward.
Accepted-answer cleanup edits that original forward to retain the plain URL
without Markdown and append the accepted static `!sop-render` block. Errors use
ordinary publication/daemon reporting, not workflow recovery. `app_url` must point
to the frontend for cross-issue links.

`context['issue_id']` and `context['workspace_id']` identify execution. Mutable
`context['state']` is stored in the issue's `sop.<name>` metadata key. Return
`{'instruction': '...'}` for guidance, `{'phase': 'destination'}` for a branch,
or `{'accepted': False, 'instruction': '...'}` to stay in the current state.
`context['cli'](*words, write=False)` calls `multica issue`; mark writes with
`write=True`. Use `context['error'](code, message)` for an actionable error.

The process keeps the caller's cwd. Use `context['sop_dir']` or
`Path(__file__).parent` for skill assets and `context['state_dir']` for local
execution files. Keep skill names at most 60 characters.

## Current-round replies and handover

An action may return `{'this_round_reply_guidance': '...'}` to replace this
invocation's `instruction` and omit all top-level `next_step` guidance, including
nested instructions and alternatives. Without the field, output is unchanged.
Transitions, destination checks, ownership and stored state still complete
normally; resume/help show ordinary guidance. This is not a provider-stop or
delivery mechanism.

On an owner shift, name the intended human/agent and where/how to reply. The
natural reply ends the turn and hands over to that recipient; do not execute
its next choice in the same turn. Guidance may embed the existing questionnaire
JSON (`status`, `instruction`, `next_step` with `multica sop run` commands).
Masking ordinary choices does not remove JSON intentionally embedded in reply text.

For strict direct publication, post that questionnaire through ordinary comment
creation (`content` is the state JSON, `as_sop: true`, with the existing thread
parent). Reuse outer-state publication rather than posting a second copy.
Return `<option> done, silence. End this round with nothing replied`, replacing
`<option>` with the option name. End without an acknowledgement; publication
errors follow recovery guidance, not success/silence guidance.

An addressed State owner supplies destination `issue_id`, `thread`, agent `id`
and native comment `content` (`/note` or an agent mention). After the action
chain, the renderer posts `!sop-render <skill-name> <owner JSON>` and ordinary,
redacted state JSON in the execution issue, not the masked invocation reply.
The execution assignee is unchanged. `--thread`/`state.reply_parent` selects the
source parent; the owner's `thread` is the forwarding destination. Forwarding
and rendering remain the Stage 3 contract.

Authoring templates remain in the CLI-owned `server/internal/cli/sopdev/`
directory as `minimal.py` and `feature-rich.py`.

## Help, validation and preview

`multica sop dev validate DIRECTORY` checks metadata, Python imports and the
exported Workflow definition. Declared normal-agent-owned to addressed (`issue_id`)
state paths require the final action to return a dictionary containing
`this_round_reply_guidance`. Earlier actions alone do not satisfy the rule.
The validator inspects declarations/source without running actions, selectors or
owner/option factories; arbitrary dynamic Python return values are not proved.
It works directly on a directory, without an issue or task context.

`multica sop run NAME --help` shows the skill body before starting, or replays
the current saved guidance. Use `multica sop docs` for authoring guidance, not a creator skill.

Config is optional and DEBUG is off by default. To enable compiled HTML previews,
add `config.json` containing `{"DEBUG": true}`. The engine derives the state
graph from Workflow; action docstrings may add local logic in fenced `dot`
blocks, as shown in the feature-rich controller. The wrapper's `ensure_command`
helper supplies Graphviz for compilation. Config and previews are published as
ownerless issue soft-links; `controller.artifacts(context)` can return ordinary
comment attachments as a filename-to-content mapping.
