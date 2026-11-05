"""Parent decisions and read-only delivery evidence."""
from sop import Option, Parameter
from functools import partial
import json
import re
from pathlib import Path, PurePosixPath
import shlex
import sys
import uuid

import review_settle as settle

REPORT_GUIDANCE = (
    "Do not write a preliminary Markdown report. At delivery report phase, prepare the required JSON "
    "(claim, metric, method, appendix; method at most 60 words/3 sentences) once. "
    "Use a short Python heredoc to write it to an ABSOLUTE /tmp path, then execute the returned "
    "publish callback with that path in the SAME bash tool call, prefixed with the bound-workdir cd. "
    "This call contains only the delivery SOP wrapper. Inspect success before the SEPARATE bare "
    "review completion call. Follow the actual report callback schema if different; never bypass "
    "verification/commit/settlement gates or manufacture measurements.")


def passed_delivery(sop, child):
    if settle.category(child) != "in_review":
        return None
    value = (child.get("metadata") or {}).get("subissue_result") or {}
    try:
        value = json.loads(value) if isinstance(value, str) else value
        if not isinstance(value, dict) or any(not isinstance(value.get(key), str) or not value[key].strip()
                                              for key in ("delivery_id", "thread_id")):
            return None
        sop.delivery.validate_answers(value.get("answers"))
    except (TypeError, ValueError):
        return None
    return value


def failed_attempt(cli, child):
    if settle.category(child) != "in_progress":
        return None
    runs = cli("runs", child["id"])
    if not runs or any(run.get("status") not in {"completed", "failed", "cancelled"}
                       or run.get("issue_id") != child["id"] for run in runs):
        return None
    latest = runs[0]  # Native issue runs are newest first, including deferred retries.
    if latest.get("status") != "failed" or latest.get("agent_id") != child.get("assignee_id") or not latest.get("id"):
        return None
    return {key: latest.get(key) for key in ("id", "status", "agent_id", "completed_at", "error", "wakeup_id")}


def cited_evidence(sop, repo, delivery, ref='HEAD'):
    """Read only small, explicitly cited tracked documents; never guess ambiguous paths."""
    text = json.dumps(delivery.get('answers', {}))
    citations = list(dict.fromkeys(re.findall(
        r'(?<![\w/.-])(?:[\w.-]+/)*[\w.-]+\.(?:md|json|txt|log)(?![\w/.-])', text)))
    if not citations or not repo:
        return []
    tree = sop.git(repo, 'ls-tree', '-r', '--name-only', ref, check=False)
    if tree.returncode:
        return [{'error': 'Cannot index committed evidence; inspect the bound repository.'}]
    paths = tree.stdout.splitlines()
    roots = {p.split('/')[0] for p in citations if '/' in p}
    scope = next(iter(roots)) + '/' if len(roots) == 1 else None
    result, remaining = [], 16000
    for citation in citations[:8]:
        matches = [p for p in paths if p == citation]
        if not matches:
            matches = [p for p in paths if PurePosixPath(p).name == PurePosixPath(citation).name]
        if len(matches) > 1 and scope:
            scoped = [p for p in matches if p.startswith(scope)]
            if scoped:
                matches = scoped
        row = {'citation': citation, 'matches': matches}
        if len(matches) == 1:
            path = matches[0]
            if any(part.lower() in {'private', 'val', 'validation', 'hidden', 'secrets'}
                   for part in PurePosixPath(path).parts):
                row['error'] = 'Restricted evidence path; do not open it.'
            else:
                blob = sop.git(repo, 'show', ref + ':' + path, check=False)
                if blob.returncode or len(blob.stdout) > min(6000, remaining):
                    row['error'] = 'Not inlined; inspect this explicit tracked path if authorized and needed.'
                else:
                    row.update(ref=ref + ':' + path, content=blob.stdout)
                    remaining -= len(blob.stdout)
        else:
            row['error'] = 'Missing or ambiguous citation; inspect or request correction, do not invent evidence.'
        result.append(row)
    return result


def parent_context(sop, repo, parent, evidence):
    """Return bound commit/status and small adjacent committed notes for one-pass review."""
    if not repo:
        return {}
    props = parent.get('metadata') or {}
    ref = 'refs/heads/' + props['git_branch'] if props.get('git_branch') else 'HEAD'
    head = sop.git(repo, 'rev-parse', '--verify', ref, check=False)
    if head.returncode:
        return {'error': 'Cannot resolve bound parent commit; verify before delivery.'}
    ref = head.stdout.strip()
    context = {'commit': ref, 'branch': props.get('git_branch'), 'workdir': props.get('git_workdir')}
    summary = sop.git(repo, 'show', '--stat', '--oneline', '--no-renames', ref, check=False)
    if summary.returncode == 0 and len(summary.stdout) <= 4000:
        context['commit_summary'] = summary.stdout
    if context['workdir']:
        status = sop.git(context['workdir'], 'status', '--short', '--branch', check=False)
        context['git_status'] = status.stdout if status.returncode == 0 else 'Unavailable; verify workdir.'
        context['clean'] = (status.returncode == 0 and not any(
            line and not line.startswith('## ') for line in status.stdout.splitlines()))
        workdir_head = sop.git(context['workdir'], 'rev-parse', '--verify', 'HEAD', check=False)
        context['workdir_commit'] = workdir_head.stdout.strip() if workdir_head.returncode == 0 else None
        context['matches_bound_commit'] = context['workdir_commit'] == ref
        context['reuse_rule'] = (
            'This read-only receipt already verifies the commit and workdir status. If clean and matching, '
            'and no later file/HEAD writes occurred, reuse it for delivery committed; no separate git '
            'status/log/show or empty commit is needed. Dirty/mismatched/changed work needs verification.')
    directories = {str(PurePosixPath(row['matches'][0]).parent) for row in evidence
                   if len(row.get('matches', [])) == 1 and 'content' in row
                   and len(PurePosixPath(row['matches'][0]).parts) > 2}
    if directories:
        tree = sop.git(repo, 'ls-tree', '-r', '--name-only', ref, check=False)
        already = {row.get('ref', '').split(':', 1)[-1] for row in evidence}
        notes, remaining = [], 6000
        for path in tree.stdout.splitlines() if tree.returncode == 0 else []:
            if (str(PurePosixPath(path).parent) not in directories or not path.endswith('.md')
                    or path in already or any(part.lower().split('.')[0] in
                    {'private', 'val', 'validation', 'hidden', 'secrets'} for part in PurePosixPath(path).parts)):
                continue
            blob = sop.git(repo, 'show', ref + ':' + path, check=False)
            if blob.returncode == 0 and len(blob.stdout) <= min(4000, remaining):
                notes.append({'ref': ref + ':' + path, 'content': blob.stdout})
                remaining -= len(blob.stdout)
            if len(notes) == 3:
                break
        context['supporting_notes'] = notes
    return context


def delivery_guidance(workdir):
    """Read the adjacent delivery policy unchanged; never synthesize or truncate it."""
    skill = Path(__file__).resolve().parent.parent / 'multica-surface-delivery' / 'SKILL.md'
    data = {'skill_path': str(skill)}
    try:
        content = skill.read_text()
    except (OSError, UnicodeError):
        data['error'] = 'Delivery guidance unavailable; read the installed SKILL before delivery.'
        return data
    if len(content) > 12000:
        data['error'] = 'Delivery guidance too large to inline; read the installed SKILL before delivery.'
        return data
    data['skill'] = content
    if workdir:
        data['callback_prefix'] = 'cd ' + shlex.quote(str(workdir)) + ' && '
        data['callback_rule'] = ('Prepend callback_prefix to EVERY delivery callback, including verified, '
                                 'committed and publish, not just entry. Review callbacks alone run unchanged.')
        data['entry_command'] = (data['callback_prefix'] +
                                 shlex.join([sys.executable, str(skill.with_name('wrapper.py'))]))
    return data


def dispatch(cli, child, decision, failure):
    script = str(Path(__file__).resolve().parent.parent / "multica-subissue-v2" / "review_settle.py")
    wakeups = cli("wakeup", "list", child["id"])
    marker = f"Sub-issue review {decision['id']}/{decision.get('attempt', 0)}:"
    previous = [row for row in wakeups if marker in row.get("instruction", "")]
    retry_failed = failure and failure["id"] != decision.get("failed_task_id") and any(
        row.get("id") == failure.get("wakeup_id") for row in previous)
    if failure and previous and failure["id"] != decision.get("failed_task_id") and not retry_failed:
        raise ValueError("Failed run does not identify the saved settlement wakeup; inspect it before retrying")
    if retry_failed or (settle.category(child) == "blocked" and decision.get("dispatched")):
        decision["attempt"] = decision.get("attempt", 0) + 1
        decision["dispatched"] = False
        if retry_failed:
            decision["failed_task_id"] = failure["id"]
        cli("metadata", "set", child["id"], "--key", settle.KEY, "--value", json.dumps(decision), write=True)
        marker = f"Sub-issue review {decision['id']}/{decision.get('attempt', 0)}:"
    if decision['action'] == "changes":
        instruction = "Fix this correction in the existing assigned workdir, then report for review: " + decision['reason']
    else:
        instruction = ("Run this exact command without changing its --decision value:\n" +
                       shlex.join(["python3", script, "--decision", decision['id']]))
    # Keep the existing reconciliation marker separate from executable arguments.
    instruction += "\n\nDispatch receipt (not a command argument): " + marker
    # The saved decision plus wakeup lookup reconciles a lost write response.
    if not any(marker in row.get("instruction", "") for row in wakeups):
        cli("status", child["id"], "in_progress", "--no-start", write=True)
        cli("wakeup", "create", child["id"], "--kind", "at", "--after", "1s",
            "--agent-id", child["assignee_id"], "--instruction", instruction, write=True)
    decision["dispatched"] = True
    cli("metadata", "set", child["id"], "--key", settle.KEY, "--value", json.dumps(decision), write=True)


def child_context(args, context):
    cli, error = context['cli'], context['error']
    parent = context['initial']
    sop = settle.branch_sop()
    child = cli("get", args.child)
    if (child.get("parent_issue_id") != parent["id"] or child["workspace_id"] != parent["workspace_id"]
            or child.get("assignee_type") != "agent" or not child.get("assignee_id")):
        raise error("invalid_child", "Child must belong to this parent/workspace and have an agent assignee.")
    pending = settle.decision(child)
    failure = failed_attempt(cli, child)
    return parent, sop, child, pending, failure


def handle(args, context):
    _, _, child, pending, failure = child_context(args, context)
    return {'data': {'child': child, 'failed_attempt': failure}}


def decide(args, context, action):
    cli, error = context['cli'], context['error']
    parent, sop, child, pending, failure = child_context(args, context)
    if action == "dispatch":
        if not pending or pending.get("phase") == "finished":
            raise error("no_dispatch", "No uncompleted dispatch intent; resume review.")
        decision = pending
    else:
        if settle.category(child) not in {"in_review", "blocked"} and not (failure and action in {"cancel", "changes"}):
            raise error("not_reviewable", "Child must be delivered, blocked, or definitively failed without an active/pending successor; failed work permits only re-steer/cancel.")
        if pending and pending.get("action") in {"accept", "cancel"} and pending.get("phase") != "finished":
            raise error("pending_settlement", "Resume dispatch for the saved decision; do not replace an unfinished settlement.")
        delivery = passed_delivery(sop, child) if action == "accept" else None
        if action == "accept" and not delivery:
            raise error("undelivered_child", "Acceptance requires in_review and a passed delivery report; re-steer, cancel, or handle the blocker first.")
        query = sop.kind(child) == "query"
        if action == "accept" and not query and (not args.commit or not args.verify or not args.verify.strip()):
            raise error("invalid_acceptance", "Acceptance requires the reviewed --commit and integration --verify command.")
        if action in {"cancel", "changes"} and (not args.reason or not args.reason.strip()):
            raise error("missing_reason", "Provide the cancellation reason or concrete correction.")
        repo = args.repo
        branch = (child.get("metadata") or {}).get("git_branch")
        target = (parent.get("metadata") or {}).get("git_branch")
        head = None
        if not query:
            repo, _ = sop.repository(repo)
            branch, target = settle.bindings(sop, repo, child, parent)
            head = sop.git(repo, "rev-parse", "refs/heads/" + branch).stdout.strip()
        if action == "accept" and not query and (args.commit != head or delivery.get("head") != head):
            raise error("changed_head", "Child head changed after delivery; report and review its current committed evidence.")
        decision = {"id": uuid.uuid4().hex, "action": action, "phase": "requested",
                    "parent_id": parent["id"], "parent_branch": target, "child_branch": branch,
                    "head": head, "repo": str(repo) if repo else "", "verify": args.verify or "", "reason": args.reason or ""}
        cli("metadata", "set", child["id"], "--key", settle.KEY,
            "--value", json.dumps(decision), write=True)
    dispatch(cli, child, decision, failure)
    workdir = (parent.get('metadata') or {}).get('git_workdir')
    prefix = 'cd ' + shlex.quote(str(workdir)) + ' && ' if workdir else None
    return {"instruction": (
            "Child resumed asynchronously. Prepare authorized parent delivery using parent_context and "
            "the already returned delivery SKILL. " + REPORT_GUIDANCE + " "
            "IMPORTANT: delivery callbacks are NOT review callbacks. "
            "Prepend data.delivery_callback_prefix to EVERY delivery command, including verified, committed "
            "and publish. Never run a delivery verified callback bare: each shell resets CWD. "
            "Review callbacks alone run exactly as returned without cd. Verify unchanged evidence once; "
            "inspect again only for a concrete gap or changed HEAD. Check review once before final "
            "publication; if still active, yield for native handoff rather than poll/re-dispatch. "
            "Start a new native task with the bare review wrapper."),
            "data": {"child_id": child["id"], "decision": decision,
            "delivery_callback_prefix": prefix, "failed_attempt": failure}}


def decision_option(name, child, prompt, *, head=None):
    params = [Parameter('--child', value=child['id']),
              Parameter('--commit', required=bool(head), value=head),
              Parameter('--verify', 'Integration command', required=bool(head)),
              Parameter('--reason', 'Concrete correction or cancellation reason', required=name in {'cancel', 'resteer'})]
    return Option(name, 'handling' if name == 'handle' else 'waiting', prompt,
                  handle if name == 'handle' else partial(decide, action='changes' if name == 'resteer' else name),
                  params=params)


def overview(context):
    args, cli = context['args'], context['cli']
    parent = context['initial']
    sop = settle.branch_sop()
    args.repo, _ = sop.resolve_repository(args.repo, parent, context['workspace_id'])
    grouped = cli('children', parent['id'])
    children = grouped['unstaged'] + [c for group in grouped['stages'] for c in group['issues']]
    rows, options = [], []
    for child in children:
        pending = settle.decision(child)
        row = {'id': child['id'], 'status': child['status'],
               'branch': (child.get('metadata') or {}).get('git_branch'), 'decision': pending}
        rows.append(row)
        if child.get('assignee_type') != 'agent' or not child.get('assignee_id'):
            continue
        failure = failed_attempt(cli, child)
        if failure:
            row['failed_attempt'] = failure
        if pending and pending.get('phase') != 'finished' and pending.get('action') in {'accept', 'cancel'}:
            if not pending.get('dispatched') or failure or settle.category(child) == 'blocked':
                options.append(decision_option('dispatch', child, 'Resume the saved settlement.'))
            continue
        if settle.category(child) not in {'in_review', 'blocked'} and not failure:
            continue
        if context['phase'] != 'handling':
            tag, delivery = sop.kind(child), passed_delivery(sop, child)
            if delivery:
                row['delivery'] = delivery
                if tag == 'query':
                    branch = (parent.get('metadata') or {}).get('git_branch')
                    row['cited_evidence'] = cited_evidence(sop, args.repo, delivery, 'refs/heads/' + branch if branch else 'HEAD')
            if delivery and tag == 'query':
                options.append(decision_option('accept', child, 'Accept the reviewed query answer.'))
            elif delivery and (tag != 'exp' or not (child.get('metadata') or {}).get('experiment_group')):
                head = sop.git(args.repo, 'rev-parse', '--verify', 'refs/heads/' + (row['branch'] or ''), check=False)
                row['head'] = head.stdout.strip() if head.returncode == 0 else None
                if row['head'] and row['head'] == delivery.get('head'):
                    options.append(decision_option('accept', child, 'Accept the reviewed commit.', head=row['head']))
            options.extend((decision_option('cancel', child, 'Cancel without merging.'),
                            decision_option('handle', child, 'Handle the blocker in parent scope.')))
        options.append(decision_option('resteer', child, 'Return a concrete correction.'))
    evidence = [item for row in rows for item in row.get('cited_evidence', [])]
    return {'data': {'children': rows, 'parent_context': parent_context(sop, args.repo, parent, evidence),
                     'parent_delivery': delivery_guidance((parent.get('metadata') or {}).get('git_workdir'))
                         if any(row.get('delivery') for row in rows) else {},
                     'repository': str(args.repo), 'workdir': (parent.get('metadata') or {}).get('git_workdir')},
            'options': options, 'children': children}
