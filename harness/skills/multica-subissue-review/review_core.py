"""Parent review reads native replies and applies decisions in its own issue."""
from functools import partial
import importlib.util
from pathlib import Path
import re
import tempfile

from sop import Option, Parameter
import integration


def branch_sop():
    path = Path(__file__).resolve().parent.parent / 'multica-subissue-distribute/controller.py'
    spec = importlib.util.spec_from_file_location('subissue_branches', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def category(issue):
    return {'done': 'done', 'closed': 'cancelled'}.get(issue.get('status_category'), issue['status'])


def children(cli, identity):
    grouped = cli('children', identity)
    return grouped['unstaged'] + [c for stage in grouped['stages'] for c in stage['issues']]


def replies(cli, child):
    return [c for c in cli('comment', 'list', child['id'], '--recent', '3')
            if not c.get('deleted_at') and c.get('content')]


def delivery_reply(comments, child):
    return next((c for c in reversed(comments) if c.get('author_type') == 'agent' and
                 c.get('author_id') == child.get('assignee_id')), None)


def reported_commit(reply):
    commits = re.findall(r'^Commit: ([0-9a-f]{40})$', reply['content'], re.M) if reply else []
    return commits[-1] if commits else None


def post(cli, identity, content):
    with tempfile.NamedTemporaryFile(mode='w', suffix='.md', dir=Path.cwd()) as stream:
        stream.write(content)
        stream.flush()
        return cli('comment', 'add', identity, '--content-file', stream.name, write=True)


def group_members(child, siblings):
    group = (child.get('metadata') or {}).get('experiment_group')
    if not group:
        return [child], 1
    expected = int(child['metadata']['experiment_group_size'])
    members = [c for c in siblings if (c.get('metadata') or {}).get('experiment_group') == group]
    if expected < 1 or len(members) > expected or any(int(c['metadata']['experiment_group_size']) != expected for c in members):
        raise ValueError('Competitive group has inconsistent membership or size')
    return members, expected


def child_context(args, context):
    child = context['cli']('get', args.child)
    parent = context['initial']
    if child.get('parent_issue_id') != parent['id'] or child['workspace_id'] != parent['workspace_id']:
        raise context['error']('invalid_child', 'Choose a direct child of this parent issue.')
    return parent, child


def handle(args, context):
    _, child = child_context(args, context)
    return {'instruction': 'Resolve this blocker in parent scope, then return a concrete correction to the child.',
            'data': {'child_id': child['id'], 'comments': replies(context['cli'], child)}}


def decide(args, context, action):
    cli, error = context['cli'], context['error']
    parent, child = child_context(args, context)
    status = category(child)
    if status not in {'in_review', 'blocked'}:
        raise error('not_reviewable', 'Review a delivered or blocked child; yield for unfinished work.')
    if action == 'accept':
        reply = delivery_reply(replies(cli, child), child)
        if status != 'in_review' or not reply:
            raise error('undelivered_child', 'Acceptance requires in_review and a native delivery reply.')
        if any(category(c) not in {'done', 'cancelled'} for c in children(cli, child['id'])):
            raise error('unfinished_children', 'Ask this child to finish its own sub-issue review first.')
        sop = branch_sop()
        if sop.kind(child) != 'query':
            if reported_commit(reply) != args.commit:
                raise error('changed_delivery', 'The reviewed commit must match the child delivery reply; request a fresh report.')
            context['external_write_attempted'] = True
            integration.accept(sop, args.repo, child, parent, args.commit, args.verify)
        terminal = 'done'
        commit = getattr(args, 'commit', None)
        message = 'Accepted after parent review.' + (' Commit: ' + commit if commit else '')
    else:
        if not args.reason or not args.reason.strip():
            raise error('missing_reason', 'Provide a concrete correction or cancellation reason.')
        terminal = 'cancelled' if action == 'cancel' else 'in_progress'
        message = args.reason.strip()
    if action == 'changes':
        if child.get('assignee_type') != 'agent' or not child.get('assignee_id'):
            raise error('unassigned_child', 'Assign the child before returning a correction.')
        cli('status', child['id'], 'in_progress', '--no-start', write=True)
        message = ('[@assignee](mention://agent/' + child['assignee_id'] + ') ' + message +
                   '\n\nContinue in this issue. Deliver through multica-surface-delivery; use multica-surface-block for an external blocker.')
        comment = post(cli, child['id'], message)
    else:
        comment = post(cli, child['id'], '/note\n' + message)
        cli('status', child['id'], terminal, '--no-start', write=True)
    return {'data': {'child_id': child['id'], 'status': terminal, 'comment_id': comment['id']}}


def decision_option(name, child, prompt, head=None):
    params = [Parameter('--child', value=child['id'])]
    if name == 'accept' and head:
        params += [Parameter('--commit', value=head), Parameter('--verify', 'Integration verification command')]
    if name in {'resteer', 'cancel'}:
        params.append(Parameter('--reason', 'Concrete correction or cancellation reason'))
    return Option(name, 'handling' if name == 'handle' else 'review', prompt,
                  handle if name == 'handle' else partial(decide, action='changes' if name == 'resteer' else name), params=params)


def overview(context):
    cli, args = context['cli'], context['args']
    parent = context['initial']
    rows, options = [], []
    sop = branch_sop()
    for child in children(cli, parent['id']):
        props = child.get('metadata') or {}
        row = {'id': child['id'], 'status': child['status'], 'branch': props.get('git_branch'),
               'label': sop.kind(child)}
        rows.append(row)
        if category(child) not in {'in_review', 'blocked'}:
            continue
        row['comments'] = replies(cli, child)
        reply = delivery_reply(row['comments'], child)
        if category(child) == 'in_review' and reply:
            head = None
            if row['label'] != 'query':
                repo, _ = sop.resolve_repository(args.repo, child, context['workspace_id'])
                head = sop.git(repo, 'rev-parse', 'refs/heads/' + row['branch']).stdout.strip()
                row['head'] = head
                if reported_commit(reply) != head:
                    row['instruction'] = 'The branch differs from its delivery reply. Request a fresh report before acceptance.'
            if not props.get('experiment_group') and (row['label'] == 'query' or reported_commit(reply) == head):
                options.append(decision_option('accept', child, 'Accept the reviewed reply and verified work.', head))
        options += [decision_option('cancel', child, 'Cancel this path without merging.'),
                    decision_option('handle', child, 'Handle its blocker in parent scope.'),
                    decision_option('resteer', child, 'Send a concrete correction to the same child.')]
    return {'data': {'children': rows}, 'options': options}
