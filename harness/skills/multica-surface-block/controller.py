"""Ask the caller's two blocking questions, then notify the parent."""
from sop import Completed, Option, Parameter, State, Workflow
import json
import tempfile
from pathlib import Path

CONFIRM = "Confirm that the issue faces a barrier outside its scope and that there is no expert agent to mention who can solve this problem."
DESCRIBE = "Describe the barrier and required help. For a child, include --parent-thread with the relevant parent request comment."


def post(context, issue_id, content, *options):
    with tempfile.NamedTemporaryFile(mode='w', suffix='.md', dir=Path.cwd()) as stream:
        stream.write(content)
        stream.flush()
        return context['cli']('comment', 'add', issue_id, '--content-file', stream.name, *options, write=True)


def block(args, context):
    issue, cli, error = context['initial'], context['cli'], context['error']
    reason = args.reason.strip()
    if not reason:
        raise error('missing_reason', 'Describe the concrete blocker and the help needed from the parent.')
    parent = cli('get', issue['parent_issue_id']) if issue.get('parent_issue_id') else None
    if parent:
        require(parent['workspace_id'] == issue['workspace_id'], 'Parent is outside the issue workspace')
        if not args.parent_thread:
            raise error('missing_parent_thread', 'Supply --parent-thread with the relevant parent request comment.')
        if parent.get('assignee_type') not in {'agent', 'squad'} or not parent.get('assignee_id'):
            raise error('missing_parent_owner', 'Assign a responsible agent or squad to the parent before handing off.')
    path = Path.cwd() / ('.multica-blocker-' + context['task_id'] + '.json')
    receipt = json.loads(path.read_text()) if path.exists() else {'reason': reason, 'parent_thread': args.parent_thread}
    require(receipt['reason'] == reason and receipt['parent_thread'] == args.parent_thread,
            'Blocker request changed; reconcile the saved handoff before retrying')

    def publish(key, identity, content, *options):
        if key not in receipt:
            if receipt.get('posting'):
                raise error('unconfirmed_post', 'A blocker comment write is uncertain; inspect the saved handoff and native thread before retrying.')
            receipt['posting'] = key
            path.write_text(json.dumps(receipt))
            receipt[key] = post(context, identity, content, *options)
            receipt.pop('posting')
            path.write_text(json.dumps(receipt))
        return receipt[key]

    comment = publish('blocker', issue['id'], '/note\n' + reason)
    data = {'comment_id': comment['id']}
    if category(issue) != 'blocked':
        cli('status', issue['id'], 'blocked', '--no-start', write=True)
    if parent:
        target_type, target_id = parent['assignee_type'], parent['assignee_id']
        link = f"[Child blocker](mention://issue/{issue['id']}) · Comment: {comment['id']}"
        request = publish('parent_request', parent['id'],
                          f"[@Parent owner](mention://{target_type}/{target_id}) Please resolve this child blocker.\n\n{reason}\n\n{link}",
                          '--parent', args.parent_thread, '--steer-active')
        outcome = next((row for row in request.get('trigger_outcomes', [])
                        if row['target_type'] == target_type and row['target_id'] == target_id), {})
        if outcome.get('status') not in {'steered', 'queued', 'coalesced', 'deferred'}:
            raise error('parent_not_notified', 'Parent help request was posted but dispatch was not confirmed; inspect comment ' + request['id'] + ' before retrying.')
        data.update(parent_comment_id=request['id'], parent_dispatch=outcome['status'])
    return {'instruction': 'Blocker posted and parent help request dispatched; await its response.' if parent else
                           'Blocker posted; await the issue owner.', 'data': data}


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


def precheck(context):
    issue = read_issue(context)
    if 'result' in context:
        require(issue['workspace_id'] == context['initial']['workspace_id'], 'SOP workspace changed')
        if context['result']['phase'] == 'finished':
            require(category(issue) in {'blocked', 'done', 'cancelled'}, 'Blocked status was not persisted')
        return issue
    context['initial'] = issue
    if category(issue) in {'done', 'cancelled'} or category(issue) == 'blocked' and not issue.get('parent_issue_id'):
        raise Completed('Issue is blocked or terminal; await its owner.')
    return issue


workflow = Workflow(
    State('confirm', CONFIRM, initial=True, precheck=precheck,
          options=(Option('confirm', 'describe', 'Confirm the barrier is outside this issue scope.'),)),
    State('describe', DESCRIBE, precheck=precheck,
          options=(Option('continue', 'finished', 'Post the blocker and request parent help through native steering.', block,
                          params=(Parameter('--reason', 'Concrete blocker and required parent help'),
                                  Parameter('--parent-thread', 'Relevant parent request comment ID', required=False))),)),
    State('finished', 'Blocker is persisted and parent dispatch is confirmed when this issue has a parent.', final=True, precheck=precheck),
)
