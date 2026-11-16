# Create a symbolic SoP

The CLI supplies the Python 3 engine. A skill owns `SKILL.md` and a
`controller.py` exporting a `Workflow`; do not copy the engine into the skill.

## Start from an example

```sh
multica sop dev init .agents/skills/my-sop --minimal
# Or, in an empty directory:
multica sop dev init .agents/skills/my-sop --feature-rich
multica sop dev validate .agents/skills/my-sop
multica sop run my-sop
```

`init` copies one canonical controller from `server/internal/cli/sopdev/`, plus
`SKILL.md` and this guide. `multica sop docs` also prints the guide.

- `minimal.py`: `ready → finish → FINISHED`. One State and one action.
- `feature-rich.py`: `ready → submit → review → accept → FINISHED`.
  Submit saves a review request, hands ownership to a named peer, and tells the
  agent caller what to relay. An outer caller supplies handoff content instead;
  the engine dispatches it and the caller ends silently.

For the feature-rich example, choose a real peer in your workspace:

```sh
multica sop run my-sop submit --reviewer-id "$REVIEWER_ID" --reviewer-name "$REVIEWER_NAME" --summary "Review the change."
```

The named reviewer runs `multica sop run my-sop` in the same checkout to read
its request, then chooses `accept`. Selecting yourself as reviewer keeps normal
State guidance instead of handing off.

## Build the controller

- Declare each `State`, `Option` action and `Branch` once. Connect their names
  with `edges=((source, destination), ...)`, never labelled triples or targets
  inside Options. Outgoing State edges are the caller's choices.
- Mark one State `initial=True`. States pause for input; actions run
  `(args, context)` callbacks. `Parameter` declares an action's required inputs.
- Every automatic path must reach a State or `FINISHED`; action-only cycles and
  dead ends are invalid. State retry loops are allowed.
- Keep a simple transition direct. Use a Branch or action chain only when
  routing needs it; return `{'phase': 'destination'}` to select a declared edge.
- Save procedure data in `context['state']`. A State's `view(context)` returns
  its live `instruction`; its `owner` names the human/agent who acts next.
- `FINISHED` or `State(..., final=True)` completes the procedure and clears its
  issue metadata. `--help` recalls saved guidance without executing actions.

## Declare preview text beside the node

Literal descriptions/prompts need no extra field. Dynamic State text needs a
literal `preview`; an action's handoff guidance preview is optional:

```python
State('review', 'Review the request.', view=review, preview='Review: {summary}.')
Option('submit', 'Request peer review', submit,
       this_round_handoff_guidance_preview='Reply to {reviewer_name}, then end this turn.')
```

Placeholders stay literal; previews never run functions. Add `config.json` with
`{"DEBUG": true}` to compile HTML after transitions. Static previews cover all
declared nodes; actual text covers only the current State and actions executed
in this invocation. Handoff guidance belongs to the action, not State `next_step`.

## Hand off to the next owner

For an agent-owned owner change, return
`{'this_round_handoff_guidance': '...'}` to tell the caller what to relay to
the next owner, or explicitly to stay silent. The invocation
omits `next_step`, but the transition and destination checks still complete.
Do not use this field for ordinary same-agent progress; keep normal State guidance.

For an outer-to-agent owner change, return
`{'this_round_handoff_content': 'Please review the change.'}`. The engine
combines that content with the next rendered State and sends one native
start/steer handoff to its agent owner. The outer caller does not post a second
comment or reply and receives only silence guidance, without choices. Ordinary
State guidance remains available on resume/help. Completion always ends quietly.

For an addressed outer State, its `owner` declares `issue_id`, `thread`, agent
`id` and native comment `content`. Fill `id` and mention the agent in `content`
only for automatic agent `sop-remote` replies; human gates use blank `id` and
mention-free `content`. The engine posts/forwards its questionnaire; return
explicit silence guidance from the final entering action. Do not publish
a second questionnaire or execute the recipient's choice in the same turn.

Default `attachment upload FILE` requires the current issue and comment and belongs to both.
For shared or frequently updated deliverables use `--independent`: the current issue is
required and soft-linked automatically; add `--attachment-id ID` to renew the same file.
