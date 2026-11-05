"""Ask the caller's two blocking questions, then notify the parent."""
from sop import Completed, Option, State, Workflow
import importlib.util
from pathlib import Path

CONFIRM = "Confirm that the issue faces a barrier outside its scope and that there is no expert agent to mention who can solve this problem."
DESCRIBE = "Describe the blocking barrier clearly and minimally before calling this script again to continue."


def branch_sop():
    path = Path(__file__).resolve().parent.parent / "multica-subissue-v2/controller.py"
    spec = importlib.util.spec_from_file_location("blocked_subissue_sop", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def block(args, context):
    issue = context['initial']
    if category(issue) != 'blocked':
        context['cli']('status', issue['id'], 'blocked', '--no-start', write=True)
    sop = branch_sop()
    context['external_write_attempted'] = True
    delivery = sop.delivery.notify_blocked(sop.base, context['cli']('get', issue['id']), issue['workspace_id'])
    return {'instruction': 'Issue marked blocked. Follow the parent handoff or wait for its response.', 'data': delivery}


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
    if category(issue) in {'done', 'cancelled'}:
        raise Completed('Issue is terminal; stop.')
    return issue


workflow = Workflow(
    State('confirm', CONFIRM, initial=True, precheck=precheck,
          options=(Option('confirm', 'describe', 'Confirm the barrier is outside this issue scope.'),)),
    State('describe', DESCRIBE, precheck=precheck,
          options=(Option('continue', 'finished', 'Confirm the barrier has been described, then notify the parent.', block),)),
    State('finished', 'Blocking status is persisted and the parent owns recovery.', final=True, precheck=precheck),
)
