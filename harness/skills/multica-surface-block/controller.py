"""Ask the caller's two blocking questions, then notify the parent."""
from sop import Completed, Option, Parameter, State, Workflow
import tempfile
from pathlib import Path

CONFIRM = "Confirm that the issue faces a barrier outside its scope and that there is no expert agent to mention who can solve this problem."
DESCRIBE = "Describe the blocking barrier clearly and minimally before calling this script again to continue."


def block(args, context):
    issue = context['initial']
    if not args.reason.strip():
        raise context['error']('missing_reason', 'Describe the concrete blocker and the help needed from the parent.')
    with tempfile.NamedTemporaryFile(mode='w', suffix='.md', dir=Path.cwd()) as stream:
        stream.write('/note\n' + args.reason.strip())
        stream.flush()
        comment = context['cli']('comment', 'add', issue['id'], '--content-file', stream.name, write=True)
    context['cli']('status', issue['id'], 'blocked', '--no-start', write=True)
    return {'instruction': 'Blocker posted. Native steering asks the parent to help; await its response.',
            'data': {'comment_id': comment['id']}}


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
    if category(issue) in {'blocked', 'done', 'cancelled'}:
        raise Completed('Issue is blocked or terminal; await its owner.')
    return issue


workflow = Workflow(
    State('confirm', CONFIRM, initial=True, precheck=precheck,
          options=(Option('confirm', 'describe', 'Confirm the barrier is outside this issue scope.'),)),
    State('describe', DESCRIBE, precheck=precheck,
          options=(Option('continue', 'finished', 'Post the blocker and request parent help through native steering.', block,
                          params=(Parameter('--reason', 'Concrete blocker and required parent help'),)),)),
    State('finished', 'Blocking status is persisted and the parent owns recovery.', final=True, precheck=precheck),
)
