"""Read-only Git checks around child-owned integration."""
import re


def bindings(sop, repo, child, parent):
    branch = sop.metadata(child).get('git_branch')
    target = sop.metadata(parent).get('git_branch')
    if not target and sop.kind(parent) == 'query' and branch:
        if not sop.owned_branch(repo, branch, child, child['workspace_id']):
            raise ValueError('Supplied root branch ownership changed')
        return branch, None
    if (not branch or not target or branch == target or
            sop.metadata(child).get('git_parent_branch') != target):
        raise ValueError('Child must have a distinct branch based on its parent branch')
    for value, name in ((child, branch), (parent, target)):
        if not sop.owned_branch(repo, name, value, child['workspace_id']):
            raise ValueError('Branch ownership does not match the issue')
    return branch, target


def prepare(sop, repo, child, parent, head, verify):
    repo, _ = sop.resolve_repository(repo, child, child['workspace_id'])
    branch, target = bindings(sop, repo, child, parent)
    if not head or not verify or not verify.strip():
        raise ValueError('Code acceptance requires the reviewed commit and integration verification command')
    if sop.git(repo, 'rev-parse', 'refs/heads/' + branch).stdout.strip() != head:
        raise ValueError('Child head changed after review; inspect its new delivery')
    for name in {branch, target or branch}:
        directory = sop.base.checkout(repo, name)
        if not directory:
            raise ValueError('The prepared issue worktrees must exist before acceptance')
        if sop.git(directory, 'status', '--porcelain').stdout.strip():
            raise ValueError('Commit or preserve dirty work before integration')
    return target or branch


def reported_head(reply, commit):
    if not reply or not commit or 'Integrated commit: ' + commit not in reply['content'].splitlines():
        return None
    heads = re.findall(r'^Integrated head: ([0-9a-f]{40})$', reply['content'], re.M)
    return heads[-1] if heads else None


def confirm(sop, repo, child, parent, commit, reply, approval):
    head = reported_head(reply, commit)
    if not head or not approval or 'Reviewed commit: ' + commit not in approval['content'].splitlines():
        raise ValueError('Settlement requires the approved commit and child integration result')
    repo, _ = sop.resolve_repository(repo, child, child['workspace_id'])
    branch, target = bindings(sop, repo, child, parent)
    target = target or branch
    if 'Integration target: ' + target not in approval['content'].splitlines():
        raise ValueError('Integration target changed after approval')
    if sop.git(repo, 'rev-parse', 'refs/heads/' + branch).stdout.strip() != commit:
        raise ValueError('Child head changed after review; inspect its new delivery')
    sop.git(repo, 'merge-base', '--is-ancestor', commit, head)
    sop.git(repo, 'merge-base', '--is-ancestor', head, 'refs/heads/' + target)
    remote = sop.git(repo, 'ls-remote', '--exit-code', 'origin', 'refs/heads/' + target).stdout.split()[0]
    sop.git(repo, 'merge-base', '--is-ancestor', head, remote)
