"""Resolve native delivery facts, render a report and reconcile its side effects."""
import hashlib
import importlib.util
import json
from pathlib import Path


def branch_sop():
    path = Path(__file__).resolve().parent.parent / 'multica-subissue-v2/controller.py'
    spec = importlib.util.spec_from_file_location('surface_subissue', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps(value, ensure_ascii=False, allow_nan=False) + '\n')
    temporary.replace(path)


def checkpoint(context):
    identity = [context[k] for k in ('workspace_id', 'issue_id', 'task_id')]
    key = hashlib.sha256(json.dumps(identity).encode()).hexdigest()
    path = Path(__file__).with_name('.deliveries') / (key + '.json')
    return path, json.loads(path.read_text()) if path.exists() else {'id': key}


def resolve(args, context, issue, previous=None):
    cli, error = context['cli'], context['error']
    runs = cli('runs', issue['id'], '--active')
    run = next((r for r in runs if r['id'] == context['task_id']), None)
    if not run or run.get('agent_id') != context['agent_id']:
        raise error('missing_run', 'Current native task is unavailable.', 'Restore the task environment; inspect its issue run.')
    if run.get('kind') in {'chat', 'autopilot'}:
        raise error('wrong_surface', 'This SOP delivers issue work.', 'Use the current chat/autopilot reply procedure; do not change issue state.')
    previous = previous or {}
    supplied = args.thread or list(previous.get('threads', {}).values())
    targets = list(dict.fromkeys([*run.get('coalesced_comment_ids', []), *([run['trigger_comment_id']] if run.get('trigger_comment_id') else [])]))
    if not targets and run.get('kind') == 'comment' and not supplied:
        raise error('missing_thread', 'The server omitted this run\'s reply target.', 'Resume with --thread ID from the native prompt, once per target; do not choose a recent thread.')
    if supplied:
        if targets and not set(supplied) <= set(targets):
            raise error('wrong_thread', 'Supplied thread differs from the native reply targets.', 'Use the targets from this run.')
        targets = targets or supplied
    threads = {}
    for target in targets:
        rows = cli('comment', 'list', issue['id'], '--thread', target, '--tail', '0')
        roots = [r for r in rows if not r.get('parent_id') and r.get('issue_id') == issue['id']]
        if len(roots) != 1:
            raise error('missing_thread', 'Reply thread could not be resolved.', 'Inspect the supplied native target before retrying.')
        threads[roots[0]['id']] = target
    attachments = []
    allow_external = args.allow_external_file or previous.get('allow_external_file', False)
    for path in args.attachment or [Path(p) for p in previous.get('attachments', [])]:
        path = path.resolve(strict=True)
        if not path.is_file() or not allow_external and not path.is_relative_to(Path.cwd().resolve()):
            raise error('attachment_scope', 'Attachment must be a file inside this workdir.', 'Use the current artifact; --allow-external-file requires an intentional external path.')
        attachments.append(str(path))
    sop = branch_sop()
    tag, props = sop.kind(issue), sop.metadata(issue)
    branch, repo = None, None
    if tag != 'query':
        repo, _ = sop.resolve_repository(None, issue, issue['workspace_id'])
        branch = props.get('git_branch')
        if not branch or not sop.owned_branch(repo, branch, issue, issue['workspace_id']):
            raise error('unowned_branch', 'No verified issue-owned feature branch.', 'Run the existing sub-issue prepare SOP before delivery.')
        if sop.git(Path.cwd(), 'branch', '--show-current').stdout.strip() != branch:
            raise error('wrong_branch', 'Current workdir is not on the issue branch.', 'Return to the existing issue worktree; do not create another branch.')
    return {'issue_id': issue['id'], 'surface': 'issue', 'tag': tag, 'threads': threads,
            'attachments': attachments, 'allow_external_file': allow_external,
            'branch': branch, 'workdir': str(Path.cwd().resolve()), 'repository': str(repo) if repo else None,
            'parent_id': issue.get('parent_issue_id'),
            'target_status': 'in_review' if issue.get('parent_issue_id') else 'done'}


def close_threads(cli, facts):
    for root, target in facts['threads'].items():
        rows = cli('comment', 'list', facts['issue_id'], '--thread', target, '--tail', '0')
        thread = next(row for row in rows if row['id'] == root)
        if not thread.get('resolved_at'):
            cli('comment', 'resolve', root, write=True)


def committed(facts, *, publish=False):
    sop, cwd = branch_sop(), Path(facts['workdir'])
    if sop.git(cwd, 'branch', '--show-current').stdout.strip() != facts['branch']:
        raise ValueError('Return to the prepared feature branch before reporting.')
    if sop.git(cwd, 'status', '--porcelain', '--untracked-files=no').stdout.strip():
        raise ValueError('Commit the issue source and evidence before reporting; tracked changes remain.')
    head = sop.git(cwd, 'rev-parse', 'HEAD').stdout.strip()
    if publish and sop.git(cwd, 'remote', 'get-url', 'origin', check=False).returncode == 0:
        sop.git(cwd, 'push', 'origin', head + ':refs/heads/' + facts['branch'])
    return head


def report(path, tag):
    if path is None:
        raise ValueError('Provide --report-file with claim, metric, method and appendix.')
    return branch_sop().delivery.validate_report(json.loads(path.read_text()), tag)


def publish(args, context, issue, path, receipt):
    cli, error = context['cli'], context['error']
    facts = receipt['facts']
    value = report(args.report_file, facts['tag'])
    if receipt.get('report') and receipt['report'] != value:
        raise error('changed_report', 'A delivery submission already uses a different report.', 'Reconcile the saved report before changing it.')
    receipt['report'] = value
    marker = '<!-- multica-delivery:' + receipt['id'] + ' -->'
    body = '\n\n'.join([value['claim'].strip(), *([] if value['metric'] is None else [str(value['metric'])]),
                        value['method'].strip(), value['appendix'].strip()])
    content = '/note\n' + body + '\n\n' + marker
    body_path = Path(facts['workdir']) / ('.multica-delivery-' + receipt['id'][:16] + '.md')
    body_path.write_text(content, encoding='utf-8')
    targets = list(facts['threads'].values()) or [None]
    comments = receipt.setdefault('comments', {})
    for target in targets:
        key = target or 'root'
        if comments.get(key):
            continue
        selector = ['--thread', target] if target else []
        rows = cli('comment', 'list', issue['id'], *selector, '--full')
        matches = [r for r in rows if marker in r.get('content', '') and r.get('parent_id') == target]
        if len(matches) > 1:
            raise error('duplicate_delivery', 'Multiple delivery comments match this checkpoint.', 'Inspect the saved comments; do not publish another.')
        if matches:
            comments[key] = matches[0]['id']
        else:
            if key in receipt.get('posting', []) and not args.retry_post:
                raise error('unconfirmed_post', 'A prior comment submission has no confirmed result.',
                            'Inspect this task and target thread. If the write failed and no request remains in flight, repeat publish with --retry-post.')
            receipt.setdefault('posting', []).append(key)
            save(path, receipt)
            command = ['comment', 'add', issue['id'], '--content-file', str(body_path)]
            if target:
                command += ['--parent', target]
            for attachment in facts['attachments']:
                command += ['--attachment', attachment]
            if facts['allow_external_file']:
                command += ['--allow-external-file']
            comments[key] = cli(*command, write=True)['id']
        save(path, receipt)
    body_path.unlink(missing_ok=True)
    close_threads(cli, {**facts, 'threads': facts['threads'] or {comments['root']: comments['root']}})
    current = cli('get', issue['id'])
    if ({"done": "done", "closed": "cancelled"}.get(current.get("status_category"), current["status"])) != facts['target_status']:
        if ({"done": "done", "closed": "cancelled"}.get(current.get("status_category"), current["status"])) in {'blocked', 'cancelled', 'done'}:
            raise error('changed_status', 'Issue changed state during delivery.', 'Inspect the current issue before continuing.')
        cli('status', issue['id'], facts['target_status'], '--no-start', write=True)
    result = {'head': receipt.get('head'), 'score': value['metric'] if facts['tag'] == 'exp' else None,
              'answers': value, 'thread_id': next(iter(facts['threads']), comments.get('root')),
              'delivery_id': receipt['id'], 'comment_ids': list(comments.values())}
    current = cli('get', issue['id'])
    stored = (current.get('metadata') or {}).get('subissue_result')
    encoded = json.dumps(result, sort_keys=True, allow_nan=False)
    if stored != encoded:
        cli('metadata', 'set', issue['id'], '--key', 'subissue_result', '--value', encoded, '--type', 'string', write=True)
    sop = branch_sop()
    if facts['parent_id']:
        result['handoff'] = sop.delivery.notify(sop.base, sop.issue(issue['id'], issue['workspace_id']), issue['workspace_id'])
    else:
        result['handoff'] = {'ready': False, 'reason': 'root complete'}
    receipt.update(finished=True, result=result)
    save(path, receipt)
    return result
