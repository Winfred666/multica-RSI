#!/usr/bin/env python3
"""Create tagged children, prepare their workdirs and report measured results."""
import argparse
from contextlib import nullcontext
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import sys

from sop import Option, Parameter, State, Workflow

spec = importlib.util.spec_from_file_location('branch_workflow', Path(__file__).with_name('branch_workflow.py'))
base = importlib.util.module_from_spec(spec)
spec.loader.exec_module(base)
for name in ('command', 'git', 'cli', 'issue', 'metadata', 'repository', 'resolve_repository', 'locked', 'inherit_sandbox', 'sandbox_prefix'):
    globals()[name] = getattr(base, name)

spec = importlib.util.spec_from_file_location('subissue_delivery', Path(__file__).with_name('delivery.py'))
delivery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(delivery)


def kind(value):
    tags = {label['name'] for label in value.get('labels', [])} & {'query', 'dev', 'exp'}
    if len(tags) > 1:
        raise ValueError('Use one lifecycle label: query, dev or exp')
    return next(iter(tags), 'query')


def resolve_repository(path, value, workspace):
    current = value
    while current:
        if metadata(current).get('git_repository') or metadata(current).get('git_branch') or kind(current) != 'query':
            return base.resolve_repository(path, value, workspace)
        parent = current.get('parent_issue_id')
        current = issue(parent, workspace) if parent else None
    if path is not None:
        return base.resolve_repository(path, value, workspace)
    return None, None


def branch_owner(value, workspace):
    branch = metadata(value).get('git_branch')
    while value.get('parent_issue_id'):
        parent = issue(value['parent_issue_id'], workspace)
        if metadata(parent).get('git_branch') != branch:
            break
        value = parent
    return value


def owned_branch(repo, branch, value, workspace):
    return base.owned_branch(repo, branch, branch_owner(value, workspace), workspace)


def prepare(repo, value, workspace):
    tag, props = kind(value), metadata(value)
    parent = issue(value['parent_issue_id'], workspace) if value.get('parent_issue_id') else None
    if tag != 'query' and props.get('git_branch') and props.get('git_base_branch'):
        return {**base.prepare(repo, value, workspace), 'owner_issue_id': value['id']}
    inherited = prepare(repo, parent, workspace) if parent else None
    if tag == 'query':
        branch = props.get('git_branch') or (inherited or {}).get('branch')
        if branch:
            base.bind(value, 'git_base_branch', (inherited or {}).get('branch') or props['git_base_branch'])
            base.bind(value, 'git_branch', branch)
            base.bind_repository(repo, value)
        profile, policy = inherit_sandbox(value, workspace)
        workdir = base.checkout(repo, branch) if branch else repo or Path.cwd()
        return {'issue_id': value['id'], 'owner_issue_id': branch_owner(value, workspace)['id'],
                'branch': branch, 'base_branch': metadata(value).get('git_base_branch'),
                'workdir': str(workdir) if workdir else None,
                'sandbox_profile': profile, 'sandbox_policy': policy}
    if parent and not inherited['branch']:
        raise ValueError('A dev/exp child needs an explicitly owned parent repository branch')
    branch = base.reserve(repo, value, workspace, inherited['branch'] if inherited else
                          git(repo, 'branch', '--show-current').stdout.strip())
    base.bind_repository(repo, value)
    profile, policy = inherit_sandbox(value, workspace)
    return {'issue_id': value['id'], 'owner_issue_id': value['id'], 'branch': branch,
            'base_branch': metadata(value)['git_base_branch'],
            'sandbox_profile': profile, 'sandbox_policy': policy}


def label(value, tag):
    environment = {**os.environ, 'MULTICA_WORKSPACE_ID': value['workspace_id']}
    def labels():
        rows = json.loads(command(['multica', 'label', 'list', '--output', 'json'], env=environment).stdout)
        return rows.get('labels', []) if isinstance(rows, dict) else rows
    rows = [row for row in labels() if row['name'] == tag]
    if not rows:
        try:
            command(['multica', 'label', 'create', '--name', tag, '--color', '#6b7280', '--output', 'json'], env=environment)
        except Exception:
            if not any(row['name'] == tag for row in labels()):
                raise
        rows = [row for row in labels() if row['name'] == tag]
    if len(rows) != 1:
        raise ValueError('Resolve ambiguous lifecycle label: ' + tag)
    value['labels'] = cli('label', 'add', value['id'], rows[0]['id'], workspace=value['workspace_id'])


def binding_for(args, context):
    value = issue(context['issue_id'], context['workspace_id'])
    args.issue, args.workspace = value['id'], value['workspace_id']
    repo, common = resolve_repository(args.repo, value, args.workspace)
    args.repo = args.repo or repo
    with locked(common / 'multica-sop/branches.lock', wait=True) if common else nullcontext():
        context['external_write_attempted'] = True
        props = metadata(value)
        if (props.get('git_branch') and props.get('git_base_branch')
                and branch_owner(value, args.workspace)['id'] == value['id']):
            binding = base.bootstrap(repo, value, args.workspace)
            profile, policy = inherit_sandbox(value, args.workspace)
            binding.update(issue_id=value['id'], owner_issue_id=value['id'],
                           sandbox_profile=profile, sandbox_policy=policy)
        else:
            binding = prepare(repo, value, args.workspace)
    return value, repo, common, binding


def prepare_action(args, context):
    value, repo, common, binding = binding_for(args, context)
    if kind(value) == 'query':
        shared = binding.get('workdir') or base.worktree(
            repo, common, {**binding, 'issue_id': binding['owner_issue_id']}, args.workspace)
        shared = Path(shared).resolve(strict=True)
        if shared != Path.cwd().resolve():
            link = Path.cwd() / 'shared'
            if os.path.lexists(link):
                if not link.is_symlink() or link.resolve() != shared:
                    raise ValueError('The task directory already has a different shared entry')
            else:
                link.symlink_to(shared, target_is_directory=True)
        binding['workdir'] = str(shared)
    return {'data': binding}


def validate_creation(args, value):
    if args.experiment_group is not None or args.experiment_group_size is not None:
        if args.tag != 'exp' or not args.experiment_group or not args.experiment_group_size or args.experiment_group_size < 1:
            raise ValueError('Competitive exp creation needs an experiment group and positive group size')
    if args.tag == 'exp':
        grouped = cli('children', value['id'], workspace=args.workspace)
        peers = grouped['unstaged'] + [c for group in grouped['stages'] for c in group['issues']]
        peers = [c for c in peers if kind(c) == 'exp' and
                 (metadata(c).get('experiment_group') or None) == args.experiment_group]
        if args.experiment_group and (len(peers) >= args.experiment_group_size or
                any(int(metadata(c)['experiment_group_size']) != args.experiment_group_size for c in peers)):
            raise ValueError('Use the fixed competitive group size; do not add extra members')
        cohort = {(float(metadata(c)['experiment_baseline']), metadata(c)['experiment_direction'])
                  for c in peers}
        if len(cohort) > 1:
            raise ValueError('Experiments must share one baseline and score direction')
        if cohort:
            baseline, direction = next(iter(cohort))
            if ((args.baseline_score is not None and args.baseline_score != baseline) or
                    (args.direction is not None and args.direction != direction)):
                raise ValueError('Use the existing experiment baseline and direction')
            args.baseline_score, args.direction = baseline, direction
        if args.baseline_score is None or not math.isfinite(args.baseline_score) or not args.direction:
            raise ValueError('The first exp requires a finite --baseline-score and --direction min|max')


def create_action(args, context):
    args.issue, args.workspace = context['issue_id'], context['workspace_id']
    value = issue(args.issue, args.workspace)
    validate_creation(args, value)
    repo, common = resolve_repository(args.repo, value, args.workspace)
    args.repo = args.repo or repo
    with locked(common / 'multica-sop/branches.lock', wait=True) if common else nullcontext():
        context['external_write_attempted'] = True
        parent_binding = prepare(repo, value, args.workspace)
        if args.tag != 'query' and parent_binding['branch'] is None:
            raise ValueError('A dev/exp child needs an explicitly owned parent repository branch')
        value, assignee = base.create_child(args, value, args.workspace)
        value = issue(value['id'], args.workspace)
        label(value, args.tag)
        if args.tag == 'exp':
            base.bind(value, 'experiment_baseline', str(args.baseline_score))
            base.bind(value, 'experiment_direction', args.direction)
            if args.experiment_group:
                base.bind(value, 'experiment_group', args.experiment_group)
                base.bind(value, 'experiment_group_size', str(args.experiment_group_size))
        binding = prepare(repo, value, args.workspace)
        if assignee:
            cli('assign', value['id'], '--to-id', assignee, '--no-start', workspace=args.workspace)
        if args.start:
            if not assignee:
                raise ValueError('Assign the prepared child before starting it')
            cli('status', value['id'], 'todo', workspace=args.workspace)
    return {'data': binding}


def exec_action(args, context):
    run = args.command
    if run[:1] == ['--']:
        run = run[1:]
    if not run:
        raise ValueError('exec needs a command after --')
    binding = prepare_action(args, context)['data']
    value = issue(args.issue, args.workspace)
    repo, common = resolve_repository(args.repo, value, args.workspace)
    token = hashlib.sha256((args.workspace + '\0' + binding['owner_issue_id']).encode()).hexdigest()
    with locked(common / 'multica-sop' / (token + '.lock')) if common and kind(value) != 'query' else nullcontext():
        if kind(value) == 'query' and binding.get('workdir'):
            cwd = Path(binding['workdir']).resolve(strict=True)
        elif binding['branch'] is None:
            cwd = Path(args.repo or binding['workdir'])
        else:
            path = base.worktree(repo, common, {**binding, 'issue_id': binding['owner_issue_id']}, args.workspace)
            cwd = path / Path(git(args.repo, 'rev-parse', '--show-prefix').stdout.strip())
        profiles = Path(os.environ['MULTICA_WORKSPACES_ROOT']) / '.sandboxes' if os.environ.get('MULTICA_WORKSPACES_ROOT') else None
        prefix = sandbox_prefix(binding['sandbox_profile'], binding['sandbox_policy'], cwd, profiles)
        result = command([*prefix, *run], cwd=cwd, check=False, timeout=None)
        binding.update(workdir=str(cwd), returncode=result.returncode, stdout=result.stdout, stderr=result.stderr)
    if binding.get('returncode', 0):
        context.update(native_result=binding, native_returncode=binding['returncode'])
        raise context['error']('command_failed', binding.get('stderr') or 'Command failed')
    return {'data': binding}


def report_action(args, context):
    args.issue, args.workspace = context['issue_id'], context['workspace_id']
    value = issue(args.issue, args.workspace)
    if args.score is not None and not math.isfinite(args.score):
        raise ValueError('Report a finite measured score')
    answers = delivery.read_answers(args.answers_file)
    if kind(value) == 'exp' and args.score is None:
        raise ValueError('An exp report requires --score from the public benchmark')
    thread = delivery.thread_root(cli, value, args.thread, args.workspace)
    if category(value) in {'done', 'cancelled'}:
        raise ValueError('Child is terminal; inspect its saved delivery instead of reporting again')
    value, repo, common, binding = binding_for(args, context)
    head = git(repo, 'rev-parse', 'refs/heads/' + binding['branch']).stdout.strip() if binding['branch'] else None
    if kind(value) != 'query' and git(repo, 'remote', 'get-url', 'origin', check=False).returncode == 0:
        git(repo, 'push', 'origin', head + ':refs/heads/' + binding['branch'])
    result = {'head': head, 'score': args.score, 'answers': answers, 'thread_id': thread['id']}
    review = metadata(value).get('subissue_review') or {}
    review = json.loads(review) if isinstance(review, str) else review
    result['delivery_id'] = delivery.delivery_fingerprint([value['id'], result,
        review.get('id') if review.get('action') == 'changes' else None])
    base.bind(value, 'subissue_result', json.dumps(result, sort_keys=True))
    if not thread.get('resolved_at'):
        cli('comment', 'resolve', thread['id'], workspace=args.workspace)
    if category(value) != 'in_review':
        cli('status', value['id'], 'in_review', '--no-start', workspace=args.workspace)
    binding['delivery'] = delivery.notify(base, issue(value['id'], args.workspace), args.workspace)
    return {'data': binding}


def require(condition, message):
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
    issue = context['cli']('get', result['issue_id'])
    require(issue['id'] == result['issue_id'] and issue['workspace_id'] == initial['workspace_id'], 'Sub-issue identity or workspace changed')
    props = issue.get('metadata') or {}
    if args.event == 'create':
        require(issue.get('parent_issue_id') == initial['id'], 'Created issue has the wrong parent')
        require({r['name'] for r in issue.get('labels', [])} & {'query', 'dev', 'exp'} == {args.tag}, 'Lifecycle tag was not persisted')
        if args.start:
            require(issue.get('assignee_id') and category(issue) in {'todo', 'in_progress'}, 'Child was not assigned and started')
    else:
        require(issue['id'] == initial['id'], 'SOP changed the target issue')
    if result.get('branch'):
        require(props.get('git_branch') == result['branch'], 'Branch binding was not persisted')
    if args.event == 'report':
        saved = props.get('subissue_result')
        saved = json.loads(saved) if isinstance(saved, str) else saved
        require(category(issue) == 'in_review' and saved and saved.get('delivery_id') and saved.get('thread_id'), 'Child report was not persisted')


def precheck(context):
    if 'result' in context:
        verify_result(context, context['args'], context['initial'], context['result']['data'])
        return
    return read_issue(context)


workflow = Workflow(
    State('ready', 'Create a tagged child, prepare its workdir, run a command there, or report verified results.',
          initial=True, precheck=precheck, options=(
        Option('create', 'finished', 'Create and prepare one child issue.', create_action, params=(
            Parameter('--spec-file', 'Child issue specification JSON', type=Path),
            Parameter('--tag', required=False, choices=('query', 'dev', 'exp'), default='query'),
            Parameter('--start', required=False, action='store_true'),
            Parameter('--baseline-score', required=False, type=float),
            Parameter('--direction', required=False, choices=('min', 'max')),
            Parameter('--experiment-group', required=False),
            Parameter('--experiment-group-size', required=False, type=int))),
        Option('prepare', 'finished', 'Prepare the inherited branch, sandbox and workdir.', prepare_action),
        Option('exec', 'finished', 'Execute a command in the issue workdir.', exec_action, params=(
            Parameter('command', 'Command after --', nargs=argparse.REMAINDER),)),
        Option('report', 'finished', 'Persist measured answers and hand off to parent review.', report_action, params=(
            Parameter('--answers-file', 'Measured answers JSON', type=Path),
            Parameter('--thread', 'Delivery thread or its evidence comment ID'),
            Parameter('--score', required=False, type=float))),
    )),
    State('finished', 'The command completed and its persisted effects passed readback.', final=True, precheck=precheck),
    params=(Parameter('--repo', required=False, type=Path),
            Parameter('--issue', required=False), Parameter('--workspace', required=False)),
)


if __name__ == '__main__':
    from wrapper import main
    sys.exit(main())
