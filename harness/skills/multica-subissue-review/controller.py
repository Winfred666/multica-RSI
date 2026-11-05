"""Review child deliveries and select measured experiment improvements."""
from sop import Completed, Option, Parameter, State, Workflow
import copy
import json
import math
from pathlib import Path
import shlex
import sys
import uuid

import review_core as core
import review_settle as settle

SELECTION = 'subissue_experiment_selection'


def result(child):
    value = (child.get('metadata') or {}).get('subissue_result', {})
    return json.loads(value) if isinstance(value, str) else value


def experiment_group(child):
    props = child.get('metadata') or {}
    return props.get('experiment_group') if any(label['name'] == 'exp' for label in child.get('labels', [])) else None


def comparison(experiments):
    first = experiments[0]['metadata']
    baseline, direction = float(first['experiment_baseline']), first['experiment_direction']
    rows = []
    for child in experiments:
        props = child['metadata']
        if float(props['experiment_baseline']) != baseline or props['experiment_direction'] != direction:
            raise ValueError('Experiments must share one baseline and score direction')
        if settle.category(child) == 'cancelled':
            continue
        measured = result(child)
        score = measured.get('score')
        if not isinstance(score, (int, float)) or not math.isfinite(score):
            raise ValueError('Each delivered experiment must report its measured score through sub-issue v2')
        rows.append({'id': child['id'], 'score': score, 'head': measured['head'], 'status': child['status']})
    improved = [r for r in rows if r['score'] < baseline] if direction == 'min' else [r for r in rows if r['score'] > baseline]
    best = (min if direction == 'min' else max)((r['score'] for r in improved), default=None)
    return {'baseline': baseline, 'direction': direction, 'experiments': rows,
            'best': [r['id'] for r in improved if r['score'] == best] or ['none']}


def complete_selection(args, context, parent, selection):
    cli = context['cli']
    for child_id in selection['children']:
        child = cli('get', child_id)
        if settle.category(child) in {'done', 'cancelled'}:
            continue
        pending = settle.decision(child)
        choice = copy.copy(args)
        choice.child = child_id
        choice.reason = 'Experiment comparison ' + selection['id']
        choice.event = 'dispatch' if pending and pending.get('reason') == choice.reason else ('accept' if child_id == selection['winner'] else 'cancel')
        choice.commit = result(child).get('head')
        choice.verify = selection['verify']
        core.decide(choice, context, choice.event)
    selection['phase'] = 'dispatched'
    cli('metadata', 'set', parent['id'], '--key', SELECTION, '--value', json.dumps(selection), write=True)
    return { 'instruction': 'Selected children resumed for settlement. Continue useful parent work or yield for their settlement handoffs.',
            'data': selection}


def group_data(args, context):
    sop = settle.branch_sop()
    grouped = context['cli']('children', context['initial']['id'])
    peers = grouped['unstaged'] + [c for group in grouped['stages'] for c in group['issues']]
    child = next((c for c in peers if c['id'] == args.child), None)
    if not child or not experiment_group(child):
        raise context['error']('missing_group', 'Select a direct child in an explicit experiment group.')
    experiments, expected = sop.delivery.group_members(child, peers)
    if len(experiments) != expected or any(settle.category(c) not in {'done', 'cancelled'} and not core.passed_delivery(sop, c) for c in experiments):
        raise context['error']('unfinished_experiments', 'The group needs passed delivery reports or terminal outcomes.')
    data = comparison(experiments)
    data['experiment_group'] = experiment_group(child)
    return child, experiments, data


def compare(args, context):
    child, experiments, data = group_data(args, context)
    context['comparison'] = (child, experiments, data)
    return {'data': data}


def select(args, context):
    _, experiments, data = group_data(args, context)
    if args.winner not in data['best']:
        raise context['error']('invalid_winner', 'Choose a best measured improvement, or none when baseline wins.')
    winner = next((c for c in experiments if c['id'] == args.winner), None)
    if winner and settle.category(winner) != 'done' and not args.verify:
        raise context['error']('missing_verification', 'Provide the integration --verify command.')
    selection = {'id': uuid.uuid4().hex, 'phase': 'requested', 'winner': args.winner,
                 'children': [c['id'] for c in experiments], 'verify': args.verify or ''}
    parent = context['initial']
    context['cli']('metadata', 'set', parent['id'], '--key', SELECTION, '--value', json.dumps(selection), write=True)
    return complete_selection(args, context, parent, selection)


def settle_selection(args, context):
    parent = context['initial']
    selection = pending_selection(context)
    if not selection:
        raise context['error']('missing_selection', 'No partial experiment settlement is pending.')
    return complete_selection(args, context, parent, selection)


def pending_selection(context):
    saved = (context['initial'].get('metadata') or {}).get(SELECTION)
    saved = json.loads(saved) if isinstance(saved, str) else saved
    return saved if saved and saved.get('phase') == 'requested' else None


def ready_groups(overview):
    sop, seen = settle.branch_sop(), set()
    for child in overview['children']:
        group = experiment_group(child)
        if not group or group in seen:
            continue
        seen.add(group)
        members, expected = sop.delivery.group_members(child, overview['children'])
        if (len(members) == expected and any(settle.category(c) == 'in_review' for c in members)
                and all(settle.category(c) in {'done', 'cancelled'} or core.passed_delivery(sop, c) for c in members)):
            yield child, members


def review_view(context, overview=None):
    saved = pending_selection(context)
    if saved:
        context['review_options'] = (Option('settle', 'waiting', 'Resume the saved selection after checking partial effects.', settle_selection),)
        return {'data': saved}
    overview = overview or core.overview(context)
    options = overview['options']
    for child, _ in ready_groups(overview):
        options.append(Option('compare', 'comparison', 'Compare experiment group ' + experiment_group(child), compare,
                              params=(Parameter('--child', value=child['id']),)))
    if not options and any(settle.category(c) not in {'done', 'cancelled'} for c in overview['children']):
        options.append(Option('wait', 'waiting', 'Yield for the native child handoff.'))
    view = {'data': overview['data']}
    if not options and overview['children']:
        overview['data']['completion_command'] = shlex.join([sys.executable, str(Path(__file__).with_name('wrapper.py'))])
        view['instruction'] = (
            'All children are terminal. Continue the parent goal if needed. Use parent_context for verified evidence '
            'and parent_delivery for the delivery entry command. Prefix every delivery callback with cd data.workdir &&. '
            'After successful parent delivery, run data.completion_command in a separate tool call. '
            'Preserve unmet-goal evidence; do not close the parent merely to exit review. ' + core.REPORT_GUIDANCE)
        if not overview['data'].get('parent_delivery'):
            overview['data']['parent_delivery'] = core.delivery_guidance(overview['data'].get('workdir'))
    context['review_options'] = options
    return view


def review_options(context):
    if 'review_options' not in context:
        review_view(context)
    return context['review_options']


def comparison_view(context):
    saved = pending_selection(context)
    if saved:
        context['comparison_options'] = (Option('settle', 'waiting', 'Resume the saved selection after checking partial effects.', settle_selection),)
        return {'data': saved}
    overview = core.overview(context)
    group = context.get('comparison')
    groups = [group] if group else [(child, members, comparison(members)) for child, members in ready_groups(overview)]
    if not groups:
        view = review_view(context, overview)
        context['comparison_options'] = context['review_options']
        return view
    options = []
    for child, members, data in groups:
        members = {c['id']: c for c in members}
        for winner in data['best']:
            options.append(Option('select', 'waiting', 'Confirm ' + winner, select, params=(
                Parameter('--child', value=child['id']), Parameter('--winner', value=winner),
                Parameter('--verify', 'Integration command', required=winner in members and
                          settle.category(members[winner]) != 'done'))))
    member_ids = {c['id'] for c in group[1]} if group else {c['id'] for c in overview['children']}
    options += [o for o in overview['options'] if any(p.flag == '--child' and p.value in member_ids for p in o.params)]
    context['comparison_options'] = options
    return {'data': group[2] if group else {**overview['data'], 'comparisons': [data for _, _, data in groups]}}


def comparison_options(context):
    if 'comparison_options' not in context:
        comparison_view(context)
    return context['comparison_options']


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
    data = result.get('data') or {}
    if result.get('phase') == 'finished':
        require(category(issue) in {'done', 'cancelled'}, 'Review ended before parent completion')
    if data.get('child_id') and data.get('decision'):
        child = context['cli']('get', data['child_id'])
        require(child.get('parent_issue_id') == issue['id'] and child['workspace_id'] == issue['workspace_id'], 'Reviewed child left the parent scope')
        saved = (child.get('metadata') or {}).get('subissue_review')
        saved = json.loads(saved) if isinstance(saved, str) else saved
        require(saved == data['decision'] and saved.get('dispatched'), 'Review decision or dispatch was not persisted')
    if data.get('phase') == 'dispatched' and data.get('children'):
        saved = (issue.get('metadata') or {}).get('subissue_experiment_selection')
        saved = json.loads(saved) if isinstance(saved, str) else saved
        require(saved == data, 'Experiment selection was not persisted')


def precheck(context):
    if 'result' in context:
        verify_result(context, context['args'], context['initial'], context['result'])
        return
    issue = read_issue(context)
    context['initial'] = issue
    if category(issue) in {'cancelled', 'done'}:
        raise Completed('Issue is terminal; stop.')
    return issue


workflow = Workflow(
    State('review', 'Review saved answers and cited evidence once, then choose a child decision. ' + core.REPORT_GUIDANCE,
          initial=True, precheck=precheck, view=review_view, options=review_options),
    State('handling', 'Repair the blocker in parent scope, then return a concrete correction.',
          precheck=precheck, view=review_view, options=review_options),
    State('comparison', 'Select the best measured improvement or keep the baseline; preserve promising corrections.',
          precheck=precheck, view=comparison_view, options=comparison_options),
    State('waiting', 'Continue useful parent work or yield for the native child settlement handoff. Do not poll.',
          precheck=precheck, options=(Option('review', 'review', 'Review the delivered child work.'),)),
    State('finished', 'Parent completion is persisted.', final=True, precheck=precheck),
    params=(Parameter('--repo', required=False, type=Path),),
)
