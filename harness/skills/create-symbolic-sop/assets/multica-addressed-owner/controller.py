"""Post-option addressed-state rendering using the CLI-owned engine."""
import json
import os

from sop import FINISHED, Option, State, Workflow


def handoff(args, context):
    return {'this_round_reply_guidance': 'Render comment published; silently end this turn.'}


workflow = Workflow(
    State('ready', 'Choose ask to render the addressed state.', initial=True,
          owner=lambda context: {'id': context['agent_id'], 'name': 'Publisher'},
          options=(Option('ask', 'handoff', 'Render the addressed state.'),)),
    Option('handoff', 'addressed', 'Finish the handoff.', handoff),
    State('addressed', 'Please review this result.',
          owner=json.loads(os.environ['SOP_RENDER_OWNER']),
          options=(Option('finish', FINISHED, 'Finish the fixture.'),)),
)
