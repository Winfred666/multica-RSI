#!/usr/bin/env python3
"""Apply the parent's saved acceptance or cancellation in the child's workdir."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys

KEY = "subissue_review"


def branch_sop():
    folder = Path(__file__).resolve().parent
    path = (folder if folder.name == "multica-subissue-v2" else folder.parent / "multica-subissue-v2") / "controller.py"
    spec = importlib.util.spec_from_file_location("subissue_branch_sop", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def category(issue):
    return {"done": "done", "closed": "cancelled"}.get(issue.get("status_category"), issue["status"])


def decision(issue):
    value = (issue.get("metadata") or {}).get(KEY)
    return json.loads(value) if isinstance(value, str) else value


def save(sop, child, value):
    sop.cli("metadata", "set", child["id"], "--key", KEY, "--value", json.dumps(value), workspace=child["workspace_id"])


def bindings(sop, repo, child, parent):
    branch = (child.get("metadata") or {}).get("git_branch")
    target = (parent.get("metadata") or {}).get("git_branch")
    if not target and sop.kind(parent) == "query" and branch:
        if not sop.owned_branch(repo, branch, child, child["workspace_id"]):
            raise ValueError("Supplied root branch ownership changed")
        return branch, None
    if sop.kind(child) == "query" and branch == target and branch:
        if not sop.owned_branch(repo, target, parent, child["workspace_id"]):
            raise ValueError("Query parent branch ownership changed")
        return branch, target
    if (not branch or not target or branch == target or
            (child.get("metadata") or {}).get("git_base_branch") != target):
        raise ValueError("Child must have a distinct branch based on its direct parent's bound branch")
    for value, name in ((child, branch), (parent, target)):
        if sop.git(repo, "check-ref-format", "refs/heads/" + name, check=False).returncode:
            raise ValueError("Invalid bound branch")
        exists = sop.git(repo, "show-ref", "--verify", "--quiet", "refs/heads/" + name, check=False).returncode == 0
        if exists and not sop.owned_branch(repo, name, value, child["workspace_id"]):
            raise ValueError("Branch ownership does not match issue")
    return branch, target


def worktrees(sop, repo):
    return [dict(line.split(" ", 1) for line in block.splitlines() if " " in line)
            for block in sop.git(repo, "worktree", "list", "--porcelain").stdout.strip().split("\n\n")]


def finish(sop, child, value):
    if child.get("parent_issue_id") != value["parent_id"]:
        raise ValueError("Child parent changed after review")
    if value["phase"] != "finished":
        value["phase"] = "finished"
        save(sop, child, value)
    return sop.delivery.notify(sop.base, sop.issue(child["id"], child["workspace_id"]), child["workspace_id"])


def settle(sop, repo, common, child, parent, value):
    if category(parent) in {"done", "cancelled"}:
        raise ValueError("Parent is terminal; do not change its branch")
    branch, target = bindings(sop, repo, child, parent)
    if (branch, target, parent["id"]) != (value["child_branch"], value["parent_branch"], value["parent_id"]):
        raise ValueError("Issue relationship or branch binding changed after review")
    terminal = "done" if value["action"] == "accept" else "cancelled"
    if category(child) in {"done", "cancelled"}:
        if category(child) != terminal:
            raise ValueError("Child already has a different terminal outcome")
        return finish(sop, child, value)
    owner = sop.branch_owner(parent, child["workspace_id"])["id"]
    token = hashlib.sha256((child["workspace_id"] + "\0" + owner).encode()).hexdigest()
    with sop.locked(common / "multica-sop" / (token + ".lock"), wait=True):
        rows = worktrees(sop, repo)
        parent_rows = [r for r in rows if r.get("branch") == "refs/heads/" + target]
        child_rows = [r for r in rows if r.get("branch") == "refs/heads/" + branch]
        if len(parent_rows) != 1:
            raise ValueError("Parent persistent worktree must already exist")
        parent_dir = Path(parent_rows[0]["worktree"])
        for row in parent_rows + child_rows:
            if sop.git(row["worktree"], "status", "--porcelain").stdout.strip():
                raise ValueError("Commit or preserve dirty work before settlement")
        exists = sop.git(repo, "show-ref", "--verify", "--quiet", "refs/heads/" + branch, check=False).returncode == 0
        if exists and sop.git(repo, "rev-parse", "refs/heads/" + branch).stdout.strip() != value["head"]:
            raise ValueError("Child head changed after review")
        if not exists and value["phase"] not in {"verified", "cleaned"}:
            raise ValueError("Reviewed child branch disappeared before settlement")
        remote = sop.git(repo, "remote", "get-url", "origin", check=False).returncode == 0
        if remote:
            published = sop.git(repo, "ls-remote", "--heads", "origin", "refs/heads/" + branch).stdout.split()
            if published and published[0] != value["head"]:
                if value["action"] != "cancel" or sop.git(
                        repo, "merge-base", "--is-ancestor", published[0], value["head"], check=False).returncode:
                    raise ValueError("Published child head changed after review")
        if value["action"] == "accept":
            if not value.get("verify"):
                raise ValueError("Acceptance needs an integration verification command")
            if value["phase"] not in {"verified", "cleaned"}:
                result = sop.git(parent_dir, "merge", "--no-edit", value["head"], check=False)
                if result.returncode:
                    if sop.git(parent_dir, "rev-parse", "--verify", "MERGE_HEAD", check=False).returncode == 0:
                        sop.git(parent_dir, "merge", "--abort")
                    raise ValueError("Merge failed; child branch retained: " + result.stderr.strip())
                sop.command(["/bin/sh", "-c", value["verify"]], cwd=parent_dir, timeout=None)
                if sop.git(parent_dir, "status", "--porcelain").stdout.strip():
                    raise ValueError("Verification changed the parent worktree; preserve/commit its result before retrying")
                value["phase"] = "verified"
                value["verified_parent_head"] = sop.git(parent_dir, "rev-parse", "HEAD").stdout.strip()
                save(sop, child, value)
            if sop.git(parent_dir, "rev-parse", "HEAD").stdout.strip() != value["verified_parent_head"]:
                raise ValueError("Parent changed after verification; review integration again")
            sop.git(parent_dir, "merge-base", "--is-ancestor", value["head"], "HEAD")
            if remote:
                sop.git(parent_dir, "push", "origin", "HEAD:refs/heads/" + target)
        else:
            # Keep committed cancellation evidence without retaining an active branch.
            sop.git(repo, "update-ref", "refs/multica/review-archive/" + child["id"], value["head"])
            value["phase"] = "verified"
            save(sop, child, value)
        with sop.locked(common / "multica-sop/branches.lock", wait=True):
            if remote and published:
                sop.git(repo, "push", "--force-with-lease=refs/heads/" + branch + ":" + published[0], "origin", ":refs/heads/" + branch)
            for row in child_rows:
                sop.git(repo, "worktree", "remove", row["worktree"])
            if exists:
                sop.git(parent_dir, "branch", "-d" if terminal == "done" else "-D", branch)
            value["phase"] = "cleaned"
            save(sop, child, value)
        sop.cli("status", child["id"], terminal, "--no-start", workspace=child["workspace_id"])
        return finish(sop, child, value)


def require(condition, message):
    # Keep gates active under python -O too.
    if not condition:
        raise AssertionError(message)


def read_issue(context):
    if not context.get('issue_id'):
        raise context['error']('missing_issue_id', 'MULTICA_ISSUE_ID is required')
    issue = context['cli']('get', context['issue_id'])
    require(issue['id'] == context['issue_id'], 'SOP issue identity changed')
    if context.get('workspace_id') and issue['workspace_id'] != context['workspace_id']:
        raise context['error']('wrong_workspace', 'Issue is outside the run workspace')
    require(bool(issue.get('workspace_id')) and bool(category(issue)), 'Incomplete Multica issue state')
    return issue


def settled(context):
    issue = read_issue(context)
    decision = (issue.get('metadata') or {}).get('subissue_review')
    decision = json.loads(decision) if isinstance(decision, str) else decision
    require(decision and decision.get('action') in {'accept', 'cancel'}, 'Settlement decision is missing')
    require(category(issue) == ('done' if decision['action'] == 'accept' else 'cancelled'), 'Child settlement status was not persisted')


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path)
    parser.add_argument("--decision")
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args(argv)
    sop = branch_sop()
    try:
        workspace, issue_id = os.environ["MULTICA_WORKSPACE_ID"], os.environ["MULTICA_ISSUE_ID"]
        gate_context = {'cli': lambda *words: sop.cli(*words, workspace=workspace),
                        'error': lambda code, message: ValueError(message),
                        'issue_id': issue_id, 'workspace_id': workspace}
        child = read_issue(gate_context)
        value = decision(child)
        if not value or (args.decision and value["id"] != args.decision) or value["action"] not in {"accept", "cancel"}:
            raise ValueError("No matching accept/cancel decision on this child")
        if child.get("assignee_id") != os.environ.get("MULTICA_AGENT_ID"):
            raise ValueError("Only the child's current assignee executes settlement")
        if category(child) in {"done", "cancelled"}:
            if category(child) != ("done" if value["action"] == "accept" else "cancelled"):
                raise ValueError("Child has a different terminal outcome")
            delivery = finish(sop, child, value)
            settled(gate_context)
            print(json.dumps({"ok": True, "phase": "finished", "status": child["status"], "delivery": delivery}))
            return 0
        if sop.kind(child) == "query":
            parent = sop.issue(child["parent_issue_id"], workspace)
            if parent["id"] != value["parent_id"] or category(parent) in {"done", "cancelled"}:
                raise ValueError("Query parent changed or is terminal")
            sop.cli("status", child["id"], "done" if value["action"] == "accept" else "cancelled", "--no-start", workspace=workspace)
            delivery = finish(sop, child, value)
            settled(gate_context)
            print(json.dumps({"ok": True, "phase": "finished", "decision": value["id"], "delivery": delivery}))
            return 0
        repo, common = sop.resolve_repository(args.repo, child, workspace)
        if (child.get("metadata") or {}).get("git_repository") != value["repository"]:
            raise ValueError("Repository differs from the reviewed decision")
        if not args.execute:
            profile, policy = sop.inherit_sandbox(child, workspace)
            profiles = Path(os.environ["MULTICA_WORKSPACES_ROOT"]) / ".sandboxes" if os.environ.get("MULTICA_WORKSPACES_ROOT") else None
            prefix = sop.sandbox_prefix(profile, policy, repo, profiles)
            result = sop.command([*prefix, sys.executable, str(Path(__file__).resolve()), "--repo", str(repo),
                                  "--decision", value['id'], "--execute"], cwd=repo, timeout=None)
            settled(gate_context)
            print(result.stdout.strip())
            return 0
        parent = sop.issue(child["parent_issue_id"], workspace)
        if not (parent.get("metadata") or {}).get("git_branch") and sop.kind(parent) == "query":
            branch, _ = bindings(sop, repo, child, parent)
            checkout = sop.base.checkout(repo, branch)
            if (parent["id"] != value["parent_id"] or category(parent) in {"done", "cancelled"}
                    or sop.git(repo, "rev-parse", branch).stdout.strip() != value["head"]):
                raise ValueError("Supplied root delivery changed after review")
            if value["action"] == "accept":
                sop.command(["/bin/sh", "-c", value["verify"]], cwd=checkout, timeout=None)
            sop.cli("status", child["id"], "done" if value["action"] == "accept" else "cancelled", "--no-start", workspace=workspace)
            delivery = finish(sop, child, value)
            settled(gate_context)
            print(json.dumps({"ok": True, "phase": "finished", "decision": value["id"], "delivery": delivery}))
            return 0
        parent = sop.issue(child["parent_issue_id"], workspace)
        token = hashlib.sha256((workspace + "\0" + child["id"]).encode()).hexdigest()
        with sop.locked(common / "multica-sop" / (token + ".lock")):
            delivery = settle(sop, repo, common, child, parent, value)
        settled(gate_context)
        print(json.dumps({"ok": True, "phase": "finished", "decision": value["id"], "delivery": delivery}))
        return 0
    except Exception as error:
        print(json.dumps({"ok": False, "error": str(error), "instruction": "Keep work and inspect the failure. Reconcile the saved decision before retrying; use multica-surface-block if an external barrier needs parent help."}))
        return 1


if __name__ == "__main__":
    sys.exit(main())
