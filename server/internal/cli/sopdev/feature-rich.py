from sop import FINISHED, Option, Parameter, State, Workflow


def submit(args, context):
    context['state'].update(
        summary=args.summary,
        reviewer={'id': args.reviewer_id, 'name': args.reviewer_name},
    )
    if args.reviewer_id == context['agent_id']:
        return {}
    if not context['agent_id']:
        return {'this_round_handoff_content': f'Please review: {args.summary}.'}
    return {'this_round_handoff_guidance': (
        f"Reply in this issue to [@{args.reviewer_name}](mention://agent/{args.reviewer_id}): "
        f"Please review: {args.summary}. Run `multica sop run {context['skill']}` "
        "in this checkout for your choices. Then end this turn; do not choose for the reviewer."
    )}


def review(context):
    return {'instruction': f"Review: {context['state']['summary']}. Choose accept when satisfied."}


workflow = Workflow(
    State('ready', 'Submit a request to a named reviewer.', initial=True,
          owner=lambda context: {'id': context['agent_id'], 'name': 'Caller'} if context['agent_id'] else None),
    Option('submit', 'Request peer review', submit,
           params=(Parameter('--reviewer-id', 'Reviewer agent UUID'),
                   Parameter('--reviewer-name', 'Reviewer name'),
                   Parameter('--summary', 'What the reviewer should check')),
           this_round_handoff_guidance_preview=(
               'Reply in this issue to [@{reviewer_name}](mention://agent/{reviewer_id}): '
               'Please review: {summary}. Run `multica sop run {skill}` in this checkout '
               'for your choices. Then end this turn; do not choose for the reviewer.')),
    State('review', 'Review the request.', view=review,
          preview='Review: {summary}. Choose accept when satisfied.',
          owner=lambda context: context['state']['reviewer']),
    Option('accept', 'Accept and complete'),
    edges=(('ready', 'submit'), ('submit', 'review'),
           ('review', 'accept'), ('accept', FINISHED)),
)
