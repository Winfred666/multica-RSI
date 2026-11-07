"""Verify and integrate a reviewed commit in the parent issue's turn."""
from pathlib import Path
import os


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


def accept(sop, repo, child, parent, head, verify):
    repo, common = sop.resolve_repository(repo, child, child['workspace_id'])
    branch, target = bindings(sop, repo, child, parent)
    if not head or not verify or not verify.strip():
        raise ValueError('Code acceptance requires the reviewed commit and integration verification command')
    with sop.locked(common / 'multica-sop/branches.lock', wait=True):
        if sop.git(repo, 'rev-parse', 'refs/heads/' + branch).stdout.strip() != head:
            raise ValueError('Child head changed after review; inspect its new delivery')
        child_dir = sop.base.checkout(repo, branch)
        parent_dir = sop.base.checkout(repo, target) if target else child_dir
        if not parent_dir or not child_dir:
            raise ValueError('The prepared issue worktrees must exist before acceptance')
        for directory in {parent_dir, child_dir}:
            if sop.git(directory, 'status', '--porcelain').stdout.strip():
                raise ValueError('Commit or preserve dirty work before integration')
        if target:
            result = sop.git(parent_dir, *sop.base.identity_args(parent_dir), 'merge', '--no-edit', head, check=False)
            if result.returncode:
                if sop.git(parent_dir, 'rev-parse', '--verify', 'MERGE_HEAD', check=False).returncode == 0:
                    sop.git(parent_dir, 'merge', '--abort')
                raise ValueError('Merge failed; child work retained: ' + result.stderr.strip())
        profile, policy = sop.inherit_sandbox(parent, parent['workspace_id'])
        profiles = Path(os.environ['MULTICA_WORKSPACES_ROOT']) / '.sandboxes' if os.environ.get('MULTICA_WORKSPACES_ROOT') else None
        prefix = sop.sandbox_prefix(profile, policy, parent_dir, profiles)
        sop.command([*prefix, '/bin/sh', '-c', verify], cwd=parent_dir, timeout=None)
        if sop.git(parent_dir, 'status', '--porcelain').stdout.strip():
            raise ValueError('Verification changed the worktree; preserve its result before accepting')
        sop.git(parent_dir, 'merge-base', '--is-ancestor', head, 'HEAD')
        if target and sop.git(repo, 'remote', 'get-url', 'origin', check=False).returncode == 0:
            sop.git(parent_dir, 'push', 'origin', 'HEAD:refs/heads/' + target)
    return head
