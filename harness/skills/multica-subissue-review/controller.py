"""Review native child replies in the parent issue."""
from sop import Completed, Option, Parameter, State, Workflow
import copy
from pathlib import Path

import review_core as core


def group_data(args, context):
    siblings = core.children(context['cli'], context['initial']['id'])
    child = next((c for c in siblings if c['id'] == args.child), None)
    if not child or not (child.get('metadata') or {}).get('experiment_group'):
        raise context['error']('missing_group', 'Choose a direct child in an explicit experiment group.')
    members, expected = core.group_members(child, siblings)
    if len(members) != expected or any(core.category(c) not in {'in_review', 'done', 'cancelled'} for c in members):
        raise context['error']('unfinished_experiments', 'Wait for every group member to deliver or finish.')
    return child, members


def compare(args, context):
    child, _ = group_data(args, context)
    context['comparison'] = child['id']


def select(args, context):
    _, members = group_data(args, context)
    if args.winner != 'none' and args.winner not in {c['id'] for c in members if core.category(c) != 'cancelled'}:
        raise context['error']('invalid_winner', 'Choose a reviewed group member or none to retain the baseline.')
    decisions, instructions = [], []
    ordered = sorted(members, key=lambda c: c['id'] != args.winner)
    for child in ordered:
        if core.category(child) in {'done', 'cancelled'}:
            continue
        choice = copy.copy(args)
        choice.child = child['id']
        choice.reason = 'Parent comparison selected ' + args.winner + '.'
        action = 'cancel'
        if child['id'] == args.winner:
            comments = core.replies(context['cli'], child)
            reply = core.delivery_reply(comments, child)
            action = 'settle' if (core.approval_reply(comments, context['initial']) and
                                  core.integration.reported_head(reply, args.commit)) else 'accept'
        result = core.decide(choice, context, action)
        decisions.append(result['data'])
        if result.get('instruction'):
            instructions.append(result['instruction'])
    return {'data': {'decisions': decisions}, **({'instruction': '\n'.join(instructions)} if instructions else {})}


def done(args, context):
    if any(core.category(c) not in {'done', 'cancelled'} for c in core.children(context['cli'], context['issue_id'])):
        raise context['error']('unfinished_children', 'Finish child review before completing this SOP.')


def review_view(context):
    overview = core.overview(context)
    children = core.children(context['cli'], context['issue_id'])
    seen = set()
    for child in children:
        group = (child.get('metadata') or {}).get('experiment_group')
        if not group or group in seen:
            continue
        seen.add(group)
        members, expected = core.group_members(child, children)
        if (len(members) == expected and any(core.category(c) == 'in_review' for c in members)
                and all(core.category(c) in {'in_review', 'done', 'cancelled'} for c in members)):
            overview['options'].append(Option('compare', 'comparison', 'Compare group ' + group + ' from its delivery replies.', compare,
                                              params=(Parameter('--child', value=child['id']),)))
    if all(core.category(c) in {'done', 'cancelled'} for c in children):
        overview['options'].append(Option('done', 'finished', 'Confirm every child is reviewed and settled.', done))
    else:
        overview['options'].append(Option('wait', 'waiting', 'Continue useful parent work or yield for native child steering.'))
    context['review_options'] = overview['options']
    return {'data': overview['data']}


def review_options(context):
    if 'review_options' not in context:
        review_view(context)
    return context['review_options']


def comparison_view(context):
    children = core.children(context['cli'], context['issue_id'])
    options, groups, seen = [], [], set()
    sop = core.branch_sop()
    for child in children:
        group = (child.get('metadata') or {}).get('experiment_group')
        if not group or group in seen or (context.get('comparison') and child['id'] != context['comparison']):
            continue
        members, expected = core.group_members(child, children)
        if len(members) != expected or any(core.category(c) not in {'in_review', 'done', 'cancelled'} for c in members):
            continue
        seen.add(group)
        groups.append({'group': group, 'baseline': child['metadata']['experiment_baseline'],
                       'direction': child['metadata']['experiment_direction'], 'children': [
                           {'id': c['id'], 'status': c['status'], 'comments': core.replies(context['cli'], c)} for c in members]})
        for member in members:
            if core.category(member) == 'cancelled':
                continue
            repo, _ = sop.resolve_repository(context['args'].repo, member, context['workspace_id'])
            head = sop.git(repo, 'rev-parse', 'refs/heads/' + member['metadata']['git_branch']).stdout.strip()
            options.append(Option('select', 'review', 'Select this measured improvement after reviewing the group.', select, params=(
                Parameter('--child', value=child['id']), Parameter('--winner', value=member['id']),
                Parameter('--commit', value=head), Parameter('--verify', 'Integration verification command'))))
        options.append(Option('select', 'review', 'Keep the baseline and cancel the competing paths.', select, params=(
            Parameter('--child', value=child['id']), Parameter('--winner', value='none'))))
    if not options:
        view = review_view(context)
        context['comparison_options'] = context['review_options']
        return view
    context['comparison_options'] = options
    return {'data': {'comparisons': groups}}


def comparison_options(context):
    if 'comparison_options' not in context:
        comparison_view(context)
    return context['comparison_options']


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def precheck(context):
    issue = context['cli']('get', context['issue_id'])
    require(issue['id'] == context['issue_id'] and issue['workspace_id'] == context['workspace_id'], 'Review issue or workspace changed')
    if 'result' in context:
        data = context['result'].get('data') or {}
        for decision in data.get('decisions', [data] if data.get('status') else []):
            child = context['cli']('get', decision['child_id'])
            require(child.get('parent_issue_id') == issue['id'] and child['workspace_id'] == issue['workspace_id'], 'Reviewed child left parent scope')
            require(core.category(child) == decision['status'], 'Child decision status was not persisted')
        if context['result']['phase'] == 'finished':
            require(all(core.category(c) in {'done', 'cancelled'} for c in core.children(context['cli'], issue['id'])), 'Child review is unfinished')
        return
    context['initial'] = issue
    if core.category(issue) in {'done', 'cancelled'}:
        raise Completed('Issue is terminal; stop.')
    return issue


workflow = Workflow(
    State('review', 'Review the child delivery replies and evidence, then accept, correct or cancel.',
          initial=True, precheck=precheck, view=review_view, options=review_options),
    State('handling', 'Resolve the blocker in parent scope and send the child a concrete correction.',
          precheck=precheck, view=review_view, options=review_options),
    State('comparison', 'Compare reported scores against the common baseline. Select the best measured improvement or keep the baseline.',
          precheck=precheck, view=comparison_view, options=comparison_options),
    State('waiting', 'Yield for native child delivery or blocker steering. Continue useful parent work; do not poll.',
          precheck=precheck, options=(Option('review', 'review', 'Review the newly delivered child work.'),)),
    State('finished', 'Child review finished. Continue the parent goal and deliver when ready.', final=True, precheck=precheck),
    params=(Parameter('--repo', required=False, type=Path),),
)
