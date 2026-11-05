"""Evidence-gated delivery and the shared native parent-review handoff."""
import hashlib
import json
import math
import os
from urllib.request import HTTPRedirectHandler, Request, build_opener


def delivery_fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, allow_nan=False).encode()).hexdigest()


def group_members(child, siblings):
    props = child.get('metadata') or {}
    group = props.get('experiment_group') if any(label['name'] == 'exp' for label in child.get('labels', [])) else None
    if not group:
        return [child], 1
    size = props.get('experiment_group_size')
    if isinstance(size, bool) or not str(size).isdigit() or int(size) < 1:
        raise ValueError('Competitive group requires a positive integer experiment_group_size')
    size = int(size)
    members = sorted((c for c in siblings if c.get('parent_issue_id') == child.get('parent_issue_id')
                      and any(label['name'] == 'exp' for label in c.get('labels', []))
                      and (c.get('metadata') or {}).get('experiment_group') == group), key=lambda c: c['id'])
    if any(str(c['metadata'].get('experiment_group_size')) != str(size) for c in members) or len(members) > size:
        raise ValueError('Competitive group has inconsistent membership or size')
    return members, size


def handoff(parent, child, siblings, cancellations):
    """Ordinary deliveries notify individually; explicit groups share one barrier."""
    props = child.get('metadata') or {}
    group = props.get('experiment_group') if any(label['name'] == 'exp' for label in child.get('labels', [])) else None
    members, size = group_members(child, siblings)
    revisions, pending = [], []
    for member in members:
        saved = (member.get('metadata') or {}).get('subissue_result') or {}
        saved = json.loads(saved) if isinstance(saved, str) else saved
        status = {"done": "done", "closed": "cancelled"}.get(member.get("status_category"), member["status"])
        if status not in {'in_review', 'done', 'cancelled'} or (status == 'in_review' and not saved.get('delivery_id')):
            pending.append(member['id'])
        revisions.append([member['id'], saved.get('delivery_id'), cancellations.get(member['id'])])
    data = {'ready': not pending and len(members) == size, 'group': group,
            'child_ids': [c['id'] for c in members], 'pending': pending, 'missing_members': size - len(members)}
    if not data['ready']:
        return data
    if ({"done": "done", "closed": "cancelled"}.get(parent.get("status_category"), parent["status"])) in {'done', 'cancelled', 'backlog'}:
        return {**data, 'ready': False, 'reason': 'parent is not runnable'}
    if parent.get('assignee_type') != 'agent' or not parent.get('assignee_id'):
        raise ValueError('Parent review needs its assigned native agent')
    key = 'multica-delivery:' + delivery_fingerprint([parent['id'], parent['assignee_id'], group, revisions])
    instruction = (key + '\nReview child ' + ', '.join(data['child_ids']) +
                   (' as competitive experiment group ' + group if group else '') +
                   ' using multica-subissue-review. Inspect the saved delivery answers and evidence; status alone is not acceptance.')
    return {**data, 'agent_id': parent['assignee_id'], 'idempotency_key': key, 'instruction': instruction}


def validate_report(value, tag=None):
    if not isinstance(value, dict) or set(value) != {'claim', 'metric', 'method', 'appendix'}:
        raise ValueError('Report must contain exactly claim, metric, method and appendix.')
    if any(not isinstance(value[k], str) or not value[k].strip() for k in ('claim', 'method', 'appendix')):
        raise ValueError('claim, method and appendix must be nonempty text.')
    metric = value['metric']
    if isinstance(metric, bool) or not (metric is None or isinstance(metric, (str, int, float))):
        raise ValueError('metric must be measured text, a finite number, or null.')
    if isinstance(metric, (int, float)) and not math.isfinite(metric) or isinstance(metric, str) and not metric.strip():
        raise ValueError('metric must be finite measured data or null.')
    if tag == 'exp' and not isinstance(metric, (int, float)):
        raise ValueError('exp requires a finite numeric measured score.')
    return value


def read_answers(path):
    if path is None:
        raise ValueError('report requires --answers-file with qualitative and quantitative acceptance answers')
    return validate_answers(json.loads(path.read_text()))


def validate_answers(answers):
    if isinstance(answers, dict) and set(answers) == {'claim', 'metric', 'method', 'appendix'}:
        return validate_report(answers)
    misses = {}
    for name in ('qualitative', 'quantitative'):
        answer = answers.get(name) if isinstance(answers, dict) else None
        if not isinstance(answer, dict) or not isinstance(answer.get('applicable'), bool):
            raise ValueError(name + ': provide applicable=true or explicit N/A with applicable=false')
        if not answer['applicable']:
            continue
        if (not isinstance(answer.get('pass'), bool) or not isinstance(answer.get('evidence'), str)
                or not answer['evidence'].strip()):
            raise ValueError(name + ': provide pass=true|false and the evidence path/link')
        if name == 'quantitative' and any(answer.get(k) is None or
                (isinstance(answer[k], str) and not answer[k].strip()) for k in ('current', 'target')):
            raise ValueError('quantitative: provide current measured values and their required target')
        if not answer['pass']:
            misses[name] = answer
    if misses:
        raise ValueError('Acceptance criteria not met: ' + json.dumps(misses, allow_nan=False) +
                         '. Continue measured work, or preserve the committed checkpoint and ask the parent to resteer/cancel through the blocked/review SOP.')
    delivery_fingerprint(answers)
    return answers


def thread_root(cli, issue, thread, workspace):
    if not thread:
        raise ValueError('Post the two evidence answers in the delivery thread, then pass its --thread ID')
    rows = cli('comment', 'list', issue['id'], '--thread', thread, '--tail', '0', workspace=workspace)
    roots = [r for r in rows if not r.get('parent_id')]
    if len(roots) != 1 or any(r.get('issue_id') != issue['id'] for r in rows):
        raise ValueError('Delivery thread does not belong to this issue')
    return roots[0]


def wake(workspace, parent_id, body):
    # Native CLI omits idempotency_key; retain the supported native receipt API.
    token = os.environ['MULTICA_TOKEN']
    if not token.startswith('mat_'):
        raise ValueError('Delivery handoff must use the reporting native task token')
    request = Request(os.environ['MULTICA_SERVER_URL'].rstrip('/') + '/api/issues/' + parent_id + '/wakeups',
                      json.dumps(body).encode(), {'Authorization': 'Bearer ' + token,
                      'X-Workspace-ID': workspace, 'Content-Type': 'application/json'}, method='POST')
    class NoRedirect(HTTPRedirectHandler):
        def redirect_request(self, request, fp, code, message, headers, url):
            return None
    try:
        with build_opener(NoRedirect).open(request, timeout=30) as response:
            return json.load(response)
    except Exception as error:
        raise RuntimeError(f'POST /api/issues/{parent_id}/wakeups: {type(error).__name__}: {error}') from error


def transition(sop, issue, workspace):
    rows = sop.cli('timeline', issue['id'], '--action', 'status_changed', '--tail', '1', workspace=workspace)
    identity = next((row['id'] for row in reversed(rows)
                     if row.get('action') == 'status_changed' and
                     row.get('details', {}).get('to') == issue['status']), None)
    if not identity:
        raise ValueError('Current status transition is unavailable; inspect the issue before retrying its handoff')
    return identity


def notify_parent(sop, parent, data, workspace):
    if not data['ready']:
        return data
    if ({"done": "done", "closed": "cancelled"}.get(parent.get("status_category"), parent["status"])) in {'done', 'cancelled', 'backlog'}:
        return {**data, 'ready': False, 'reason': 'parent is not runnable'}
    if parent.get('assignee_type') != 'agent' or not parent.get('assignee_id'):
        raise ValueError('Parent handoff needs its assigned native agent')
    data = {**data, 'agent_id': parent['assignee_id']}
    if os.environ.get('MULTICA_ISSUE_ID') == parent['id'] and os.environ.get('MULTICA_AGENT_ID') == parent['assignee_id']:
        return {**data, 'notification': 'returned_to_parent'}
    def existing():
        return next((w for w in sop.cli('wakeup', 'list', parent['id'], workspace=workspace)
                     if w.get('agent_id') == data['agent_id'] and
                     w.get('instruction', '').split('\n', 1)[0] == data['idempotency_key']), None)
    receipt = existing()
    if receipt is None:
        body = {k: data[k] for k in ('agent_id', 'idempotency_key', 'instruction')}
        try:
            receipt = wake(workspace, parent['id'], {**body, 'kind': 'at', 'after_seconds': 1})
        except Exception as error:
            try:
                receipt = existing()
            except Exception as reconciliation:
                raise RuntimeError(f'{error}; wakeup readback failed: {reconciliation}') from error
            if receipt is None:
                raise
    return {**data, 'wakeup_id': receipt['id']}


def notify(sop, child, workspace):
    if not child.get('parent_issue_id'):
        return {'ready': False, 'reason': 'issue has no parent'}
    parent = sop.issue(child['parent_issue_id'], workspace)
    grouped = sop.cli('children', parent['id'], workspace=workspace)
    siblings = grouped['unstaged'] + [c for stage in grouped['stages'] for c in stage['issues']]
    terminal = lambda issue: ({"done": "done", "closed": "cancelled"}.get(issue.get("status_category"), issue["status"])) in {'done', 'cancelled'}
    if siblings and all(terminal(c) for c in siblings):
        observed = sorted((c['id'], transition(sop, c, workspace)) for c in siblings)
        key = 'multica-settlement:' + delivery_fingerprint([parent['id'], parent.get('assignee_id'), observed])
        data = {'ready': True, 'child_ids': [row[0] for row in observed], 'idempotency_key': key,
                'instruction': key + '\nAll direct children are settled: ' + ', '.join(row[0] for row in observed) +
                '. Inspect their final evidence and continue the parent issue toward its goal.'}
    else:
        members, _ = group_members(child, siblings)
        if terminal(child) and not (any(label['name'] == 'exp' for label in child.get('labels', []))
                                    and (child.get('metadata') or {}).get('experiment_group')):
            return {'ready': False, 'reason': 'other children remain open'}
        cancellations = {c['id']: transition(sop, c, workspace) for c in members
                         if ({"done": "done", "closed": "cancelled"}.get(c.get("status_category"), c["status"])) == 'cancelled'}
        data = handoff(parent, child, siblings, cancellations)
    return notify_parent(sop, parent, data, workspace)


def notify_blocked(sop, child, workspace):
    if not child.get('parent_issue_id'):
        return {'ready': False, 'reason': 'issue has no parent'}
    parent = sop.issue(child['parent_issue_id'], workspace)
    key = 'multica-blocked:' + delivery_fingerprint([parent['id'], parent.get('assignee_id'), child['id'],
                                                transition(sop, child, workspace)])
    return notify_parent(sop, parent, {'ready': True, 'child_ids': [child['id']], 'idempotency_key': key,
        'instruction': key + '\nChild ' + child['id'] + ' is blocked. Read its issue context for the blocker; '
        'use multica-subissue-review to continue parent review.'}, workspace)
