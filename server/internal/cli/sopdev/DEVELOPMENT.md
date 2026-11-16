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
  caller what to reply. The caller posts that reply and ends the turn.

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
literal `preview`; an action's reply preview is optional:

```python
State('review', 'Review the request.', view=review, preview='Review: {summary}.')
Option('submit', 'Request peer review', submit,
       this_round_reply_guidance_preview='Reply to {reviewer_name}, then end this turn.')
```

Placeholders stay literal; previews never run functions. Add `config.json` with
`{"DEBUG": true}` to compile HTML after transitions. Static previews cover all
declared nodes; actual text covers only the current State and actions executed
in this invocation. Reply guidance belongs to the action, not State `next_step`.

## Hand off with a reply, not status feedback

Return `{'this_round_reply_guidance': '...'}` to tell the caller what to reply to
a named peer/external recipient, or explicitly to stay silent. The invocation
omits `next_step`, but the transition and destination checks still complete.
Do not use this field for ordinary same-agent progress; keep normal State guidance.

For an addressed outer State, its `owner` declares `issue_id`, `thread`, agent
`id` and native comment `content`. The engine posts/forwards its questionnaire;
return explicit silence guidance from the final entering action. Do not publish
a second questionnaire or execute the recipient's choice in the same turn.
