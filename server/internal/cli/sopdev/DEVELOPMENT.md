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
  issue metadata. `--help` recalls guidance and refreshes the current questionnaire
  without executing actions.

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
`id` and native comment `content`. Fill `id` and mention the agent in `content`
only for automatic agent `sop-remote` replies; human gates use blank `id` and
mention-free `content`. The engine publishes in `--thread` or the invoking comment
thread, never a new root. It handles forwarding and retries, merges accepted
answer `args` into the existing question, and clears pending metadata IDs without
deleting the question or its forward. On answer, a cross-issue forward becomes a
compact source link and a separate static answered card is posted in the same
target thread. Forward links are relative and do not require `app_url`. Help
refreshes the existing pending card.
Return explicit silence guidance from the final entering action. Controllers
must not publish questionnaires, manage their comments or choose for recipients.

Default `attachment upload FILE` requires the current issue and comment and belongs to both.
For shared or frequently updated deliverables use `--independent`: the current issue is
required and soft-linked automatically; add `--attachment-id ID` to renew the same file.
