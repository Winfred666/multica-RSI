"""Verified issue delivery through explicit options and read-only state checks."""
import importlib.util
from pathlib import Path
import shlex
import sys
from sop import Completed, Option, Parameter, State, Workflow

spec = importlib.util.spec_from_file_location('surface_deliver', Path(__file__).with_name('deliver.py'))
delivery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(delivery)

WRITING = ('State confirmed results directly in concise, active prose. Avoid filler and em dashes. '
           'Keep technical pointers in the appendix; never invent measurements.')
REPORT = ('Write a JSON file containing exactly claim (confirmed achievement and outcome), metric (measured result; '
          'nullable for query/dev, a finite numeric score for exp), method (plain-English cause and effect, at most '
          '60 words / 3 sentences), and appendix (reproduction commands, commits, artifacts and logs). ' + WRITING)


def finished(context, status, instruction, data=None):
    if status in {'done', 'cancelled'}:
        children = context['cli']('children', context['issue_id'])
        if children['unstaged'] or any(stage['issues'] for stage in children['stages']):
            review = Path(__file__).parent.parent / 'multica-subissue-review/wrapper.py'
            command = shlex.join([sys.executable, str(review)])
            instruction = f'Run `{command}` to finish this issue\'s child review, then end without another reply.'
    return {'instruction': instruction, 'data': data or {}}


def inspect(context):
    issue = read_issue(context)
    context['initial'] = issue
    status = category(issue)
    path, receipt = delivery.checkpoint(context)
    if status == 'blocked':
        raise context['error']('blocked', 'Issue is blocked.', 'Use the blocked SOP and parent recovery path.')
    if status == 'cancelled' or (status in {'in_review', 'done'} and (receipt.get('finished') or not receipt.get('report'))):
        result = finished(context, status, 'Issue already delivered or cancelled; end without another reply.', receipt.get('result'))
        raise Completed(result['instruction'], result['data'])
    context['delivery'] = issue, path, receipt
    return issue


def check_view(context):
    issue, path, receipt = context['delivery']
    facts = delivery.resolve(context['args'], context, issue, receipt.get('facts'))
    receipt['facts'] = facts
    delivery.save(path, receipt)
    context['facts'] = facts
    return {'data': facts}


def verified(args, context):
    issue, path, receipt = context['delivery']
    facts = delivery.resolve(args, context, issue, receipt.get('facts'))
    if receipt.get('facts') != facts:
        raise context['error']('changed_context', 'Delivery context changed after self-check.', 'Resume to inspect it before confirming.')
    receipt['verified'] = True
    delivery.save(path, receipt)


def verified_options(context):
    facts = context.get('facts') or context['delivery'][2].get('facts')
    if facts is None:
        facts = check_view(context)['data']
    return (Option('verified', 'report' if facts['tag'] == 'query' else 'commit',
                   'Confirm the requirements and evidence have passed the self-check.', verified),)


def committed(args, context):
    _, path, receipt = context['delivery']
    receipt['head'] = delivery.committed(receipt['facts'], publish=True)
    delivery.save(path, receipt)


def recheck(args, context):
    issue, path, receipt = context['delivery']
    if receipt.get('posting') or receipt.get('comments'):
        raise context['error']('delivery_started', 'Comment publication has already started.', 'Reconcile the saved delivery.')
    receipt = {key: receipt[key] for key in ('id', 'facts')}
    context['delivery'] = issue, path, receipt
    delivery.save(path, receipt)


def publish(args, context):
    issue, path, receipt = context['delivery']
    facts = receipt['facts']
    if not receipt.get('verified'):
        raise context['error']('unverified', 'Self-check has not passed.')
    if facts['tag'] != 'query' and delivery.committed(facts) != receipt['head']:
        raise context['error']('changed_commit', 'The branch changed after preparation.', 'Use recheck to verify the new evidence.')
    result = delivery.publish(args, context, issue, path, receipt)
    return finished(context, facts['target_status'], 'Delivery recorded. End without another reply; parent review owns the next decision.',
                    {'issue_id': issue['id'], 'status': facts['target_status'], 'comment_ids': result['comment_ids']})


def commit_view(context):
    _, _, receipt = context['delivery']
    facts = receipt['facts']
    return {'data': {'branch': facts['branch'], 'workdir': facts['workdir']}}


def require(condition, message):
    # Keep gates active under python -O too.
    if not condition:
        raise AssertionError(message)


def category(issue):
    return {"done": "done", "closed": "cancelled"}.get(issue.get("status_category"), issue["status"])


def read_issue(context):
    if not context.get('issue_id'):
        raise context['error']('missing_issue_id', 'MULTICA_ISSUE_ID is required')
    issue = context['cli']('get', context['issue_id'])
    require(issue['id'] == context['issue_id'], 'SOP issue identity changed')
    if context.get('workspace_id') and issue['workspace_id'] != context['workspace_id']:
        raise context['error']('wrong_workspace', 'Issue is outside the run workspace')
    require(bool(issue.get('workspace_id')) and bool(category(issue)), 'Incomplete Multica issue state')
    return issue


def verify_result(context, args, initial, result):
    issue = read_issue(context)
    require(issue['workspace_id'] == initial['workspace_id'], 'SOP workspace changed')
    if result.get('phase') == 'finished':
        require(category(issue) in {'in_review', 'done', 'cancelled'}, 'Delivery status was not persisted')
    if args.event == 'publish' and result.get('data', {}).get('comment_ids'):
        require(category(issue) == result['data']['status'], 'Delivery status differs from the promised result')
        comments = context['cli']('comment', 'list', issue['id'], '--full')
        require(set(result['data']['comment_ids']) <= {c['id'] for c in comments}, 'Delivery reply was not posted')


def precheck(context):
    if 'result' in context:
        verify_result(context, context['args'], context['initial'], context['result'])
        return
    return inspect(context)


workflow = Workflow(
    State('check', 'Check requirements, reply targets, attachments, branch, artifacts and evidence under unchanged grading criteria.',
          initial=True, precheck=precheck, view=check_view, options=verified_options),
    State('commit', 'Commit source and evidence to the owned feature branch, preserving failed experiments.', precheck=precheck,
          view=commit_view, options=(Option('committed', 'report', 'Confirm the issue commit is ready.', committed),
                                    Option('recheck', 'check', 'Recheck changed requirements or evidence.', recheck))),
    State('report', REPORT, precheck=precheck, options=(
        Option('publish', 'finished', 'Publish the verified delivery.', publish, params=(
            Parameter('--report-file', 'Absolute path to the delivery JSON', type=Path),
            Parameter('--retry-post', required=False, action='store_true'))),
        Option('recheck', 'check', 'Recheck changed evidence before publication.', recheck))),
    State('finished', 'Delivery reply is posted; native steering brings child work to parent review.', final=True, precheck=precheck),
    params=(Parameter('--thread', required=False, action='append', default=[]),
            Parameter('--attachment', required=False, action='append', type=Path, default=[]),
            Parameter('--allow-external-file', required=False, action='store_true')),
)
