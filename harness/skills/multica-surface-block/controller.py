"""Ask the caller's two blocking questions, then notify the parent."""
from sop import Completed, Option, Parameter, State, Workflow
import tempfile
from pathlib import Path

CONFIRM = "Confirm that the issue faces a barrier outside its scope and that there is no expert agent to mention who can solve this problem."
DESCRIBE = "State the blocker and the exact help needed in a short sentence. For a child, use --parent-thread for the original assignment thread on its parent when one exists; otherwise request help in a new parent thread."


def post(context, issue_id, content, *options):
    with tempfile.NamedTemporaryFile(mode='w', suffix='.md', dir=Path.cwd()) as stream:
        stream.write(content)
        stream.flush()
        return context['cli']('comment', 'add', issue_id, '--content-file', stream.name, *options, write=True)


def block(args, context):
    """```dot
    digraph block {
        rankdir=LR; node [shape=box];
        "Validate reason and parent owner" -> "Reconcile saved handoff"
          -> "Persist blocked status" -> "Has parent?";
        "Has parent?" [shape=diamond];
        "Has parent?" -> "Original assignment thread?" [label="yes"];
        "Original assignment thread?" [shape=diamond];
        "Original assignment thread?" -> "Steer there" [label="yes"];
        "Original assignment thread?" -> "Steer in new parent thread" [label="no"];
        "Steer there" -> "Verify dispatch" -> "Finish without posting a receipt";
        "Steer in new parent thread" -> "Verify dispatch";
        "Has parent?" -> "Finish without posting a receipt" [label="no"];
    }
    ```"""
    issue, cli, error = context['initial'], context['cli'], context['error']
    reason = args.reason.strip()
    if not reason:
        raise error('missing_reason', 'Describe the concrete blocker and the help needed from the parent.')
    parent = cli('get', issue['parent_issue_id']) if issue.get('parent_issue_id') else None
    if parent:
        require(parent['workspace_id'] == issue['workspace_id'], 'Parent is outside the issue workspace')
        if parent.get('assignee_type') not in {'agent', 'squad'} or not parent.get('assignee_id'):
            raise error('missing_parent_owner', 'Assign a responsible agent or squad to the parent before handing off.')
    receipt = context['state'].setdefault('handoff', {'reason': reason, 'parent_thread': args.parent_thread})
    require(receipt['reason'] == reason and receipt['parent_thread'] == args.parent_thread,
            'Blocker request changed; reconcile the saved handoff before retrying')

    def publish(key, identity, content, *options):
        if key not in receipt:
            if receipt.get('posting'):
                raise error('unconfirmed_post', 'A blocker comment write is uncertain; inspect the saved handoff and native thread before retrying.')
            receipt['posting'] = key
            context['save_state']()
            posted = post(context, identity, content, *options)
            receipt[key] = {field: posted[field] for field in ('id', 'trigger_outcomes') if field in posted}
            receipt.pop('posting')
            context['save_state']()
        return receipt[key]

    if category(issue) != 'blocked':
        cli('status', issue['id'], 'blocked', '--no-start', write=True)
    if parent:
        target_type, target_id = parent['assignee_type'], parent['assignee_id']
        link = f"[{issue.get('identifier') or issue['id']}](mention://issue/{issue['id']})"
        thread = ('--parent', args.parent_thread) if args.parent_thread else ()
        request = publish('parent_request', parent['id'],
                          f"[@Parent owner](mention://{target_type}/{target_id}) {link}: {reason}",
                          *thread, '--steer-active')
        outcome = next((row for row in request.get('trigger_outcomes', [])
                        if row['target_type'] == target_type and row['target_id'] == target_id), {})
        if outcome.get('status') not in {'steered', 'queued', 'coalesced', 'deferred'}:
            raise error('parent_not_notified', 'Parent help request was posted but dispatch was not confirmed; inspect comment ' + request['id'] + ' before retrying.')


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
          options=(Option('continue', 'finished', 'Mark blocked and send one concise parent help request through native steering.', block,
                          params=(Parameter('--reason', 'Concrete blocker and required parent help'),
                                  Parameter('--parent-thread', 'Original assignment comment on the parent, when available', required=False))),)),
    State('finished', 'Issue is blocked; parent help was requested when applicable.', final=True, precheck=precheck),
)
