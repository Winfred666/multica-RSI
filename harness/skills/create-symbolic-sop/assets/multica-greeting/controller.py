"""Greeting handover using the existing state questionnaire wire format."""
import json
import shlex

from common import request
from sop import FINISHED, Option, Parameter, State, Workflow


def greet(args, context):
    state = workflow.states['awaiting']
    question = {
        'status': state.name,
        'instruction': f'{args.recipient}: {state.description}',
        'next_step': [
            {'instruction': option.prompt,
             'command': shlex.join(['multica', 'sop', 'run', context['skill'], *option.callback()])}
            for option in state.options],
    }
    payload = json.dumps(question)
    if args.event == 'publish':
        context['external_write_attempted'] = True
        request('/api/issues/' + context['issue_id'] + '/comments',
                json.dumps({'content': payload, 'as_sop': True,
                            'parent_id': context['state'].get('reply_parent')}).encode(),
                'application/json')
        guidance = 'publish done, silence. End this round with nothing replied'
    else:
        guidance = (f'Reply to {args.recipient} in this issue thread with exactly this JSON, '
                    'then end this turn so they can select/fill the questionnaire:\n' + payload)
    return {'this_round_reply_guidance': guidance}


def answer(args, context):
    return {'this_round_reply_guidance':
            f'Reply to the greeting sender in this issue thread: {args.greeting}. End this turn.'}


workflow = Workflow(
    State('ready', 'Choose how to greet the recipient.', initial=True, options=(
        Option('greet', 'awaiting', 'Hand over a questionnaire in your natural reply.', greet,
               params=(Parameter('--recipient', 'Human or agent to greet'),)),
        Option('publish', 'awaiting', 'Publish directly and stay silent.', greet,
               params=(Parameter('--recipient', 'Human or agent to greet'),)),
    )),
    State('awaiting', 'Hello! Please return a greeting.', options=(
        Option('answer', 'received', 'Return your greeting.', answer,
               params=(Parameter('--greeting', 'Your greeting'),)),
    )),
    State('received', 'Greeting sender: review the reply, then finish.', options=(
        Option('finish', FINISHED, 'Finish the greeting.'),
    )),
)
