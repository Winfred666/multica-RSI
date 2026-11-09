#!/usr/bin/env python3
"""Issue branch ownership, workdirs and inherited sandbox policy."""

from contextlib import contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

from wrapper import ensure_command


class WorkflowError(Exception):
    pass


def command(argv, cwd=None, check=True, timeout=90, env=None):
    result = subprocess.run(argv, cwd=cwd, text=True, capture_output=True, timeout=timeout, env=env)
    if check and result.returncode:
        raise WorkflowError(f"{argv[0]} failed: {result.stderr.strip() or result.stdout.strip()}")
    return result


def git(repo, *args, check=True):
    return command(["git", "-C", str(repo), *args], check=check)


def cli(*args, workspace):
    environment = {**os.environ, "MULTICA_WORKSPACE_ID": workspace}
    return json.loads(command(["multica", "issue", *args, "--output", "json"], env=environment).stdout)


def issue(ref, workspace):
    value = cli("get", ref, workspace=workspace)
    if value.get("workspace_id") != workspace:
        raise WorkflowError("Issue is outside the selected workspace")
    return value


def metadata(value):
    return value.get("metadata") or {}


def bind(value, key, content):
    if metadata(value).get(key) != content:
        cli("metadata", "set", value["id"], "--key", key, "--value", content, "--type", "string",
            workspace=value["workspace_id"])
        value["metadata"] = metadata(value)
        value["metadata"][key] = content


def identity_args(repo):
    email = git(repo, "config", "user.email", check=False)
    return [] if email.returncode == 0 and email.stdout.strip() else [
        "-c", "user.name=Multica Agent", "-c", "user.email=agent@multica.local"]


def branch_name(value, workspace):
    label = re.sub("[^a-z0-9]+", "-", value.get("identifier", "").strip().lower()).strip("-")
    label = label[:30].rstrip("-") or "agent"
    digest = hashlib.sha256((workspace + "\0\0" + value["id"]).encode()).hexdigest()[:12]
    return f"issue/{label}-{digest}"


def record_branch(repo, branch, base, value, workspace):
    args = identity_args(repo)
    tree = git(repo, "rev-parse", base + "^{tree}").stdout.strip()
    state = git(repo, *args, "commit-tree", tree, "-p", base,
                "-m", "multica: branch base").stdout.strip()
    message = ("multica: task branch record\n\n"
               f"Multica-Workspace: {workspace}\nMultica-Agent: \n"
               f"Multica-Conversation: {value['id']}\n")
    record = git(repo, *args, "commit-tree", tree, "-p", state, "-p", base,
                 "-m", message).stdout.strip()
    git(repo, "update-ref", "refs/multica/local-state/" + branch, record)


def owned_branch(repo, branch, value, workspace):
    ref = "refs/multica/local-state/" + branch
    record = git(repo, "rev-parse", "--verify", "--quiet", ref, check=False)
    if record.returncode:
        return False
    body = git(repo, "log", "-1", "--format=%B", record.stdout.strip()).stdout
    trailers = dict(line.split(":", 1) for line in body.splitlines() if ":" in line)
    expected = {"Multica-Workspace": workspace, "Multica-Agent": "",
                "Multica-Conversation": value["id"]}
    if any(trailers.get(key, "\0").strip() != content for key, content in expected.items()):
        return False
    checkpoint = git(repo, "rev-parse", "--verify", ref + "^2", check=False)
    return checkpoint.returncode == 0 and git(
        repo, "merge-base", "--is-ancestor", checkpoint.stdout.strip(),
        "refs/heads/" + branch, check=False).returncode == 0


@contextmanager
def locked(path, *, wait=False):
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a") as stream:
        try:
            flags = fcntl.LOCK_EX
            if not wait:
                flags |= fcntl.LOCK_NB
            fcntl.flock(stream, flags)
        except BlockingIOError:
            raise WorkflowError("Another SOP operation holds this repository/issue lock") from None
        yield


def repository(path):
    root = Path(git(path, "rev-parse", "--show-toplevel").stdout.strip()).resolve()
    common = Path(git(root, "rev-parse", "--path-format=absolute", "--git-common-dir").stdout.strip())
    return root, common


def resolve_repository(path, value, workspace):
    """Resolve local execution resources; persist only the repository URL."""
    current, expected, project = value, None, None
    while current:
        expected = expected or metadata(current).get("git_repository")
        project = project or current.get("project_id")
        parent = current.get("parent_issue_id")
        current = issue(parent, workspace) if parent else None
    if path is None and project:
        result = command(["multica", "project", "resource", "list", project, "--output", "json"],
                         env={**os.environ, "MULTICA_WORKSPACE_ID": workspace})
        resources = json.loads(result.stdout)["resources"]
        daemon = os.environ.get("MULTICA_DAEMON_ID")
        local = [r["resource_ref"]["local_path"] for r in resources
                 if r["resource_type"] == "local_directory"
                 and (not daemon or r["resource_ref"]["daemon_id"] == daemon)]
        if len(local) == 1:
            path = local[0]
    repo, common = repository(path or Path.cwd())
    if expected and git(repo, "remote", "get-url", "origin").stdout.strip() != expected:
        raise WorkflowError("Checkout differs from the issue's Git repository")
    return repo, common


def bind_repository(repo, value):
    remote = git(repo, "remote", "get-url", "origin", check=False)
    if remote.returncode == 0:
        url = remote.stdout.strip()
        if url.startswith(("/", "file:")):
            raise WorkflowError("Git delivery requires a repository URL")
        bind(value, "git_repository", url)


def checkout(repo, branch):
    for block in git(repo, "worktree", "list", "--porcelain").stdout.strip().split("\n\n"):
        row = dict(line.split(" ", 1) for line in block.splitlines() if " " in line)
        if row.get("branch") == "refs/heads/" + branch:
            return Path(row["worktree"])
    return None


def reserve(repo, value, workspace, base):
    bound = metadata(value).get("git_branch", "")
    branch = bound or branch_name(value, workspace)
    if git(repo, "check-ref-format", "refs/heads/" + branch, check=False).returncode:
        raise WorkflowError(f"Invalid issue branch {branch!r}")
    exists = git(repo, "rev-parse", "--verify", "--quiet", "refs/heads/" + branch, check=False)
    if exists.returncode == 0:
        if not owned_branch(repo, branch, value, workspace):
            raise WorkflowError(f"Branch {branch!r} is not owned by this issue")
    else:
        if bound:
            raise WorkflowError(f"Bound issue branch {branch!r} is missing from this repository")
        if git(repo, "check-ref-format", "refs/heads/" + base, check=False).returncode:
            raise WorkflowError(f"Invalid parent branch {base!r}")
        checkpoint = git(repo, "rev-parse", "--verify", "refs/heads/" + base + "^{commit}").stdout.strip()
        git(repo, "branch", "--no-track", branch, checkpoint)
        try:
            record_branch(repo, branch, checkpoint, value, workspace)
        except Exception:
            git(repo, "branch", "-D", branch, check=False)
            git(repo, "update-ref", "-d", "refs/multica/local-state/" + branch, check=False)
            raise
    if exists.returncode != 0 and git(repo, "remote", "get-url", "origin", check=False).returncode == 0:
        git(repo, "push", "--set-upstream", "origin", f"refs/heads/{branch}:refs/heads/{branch}")
    # The presence of git_branch publishes the complete branch/base binding.
    bind(value, "git_parent_branch", base)
    bind(value, "git_branch", branch)
    return branch


def parse_policy(raw):
    if not isinstance(raw, str):
        raise WorkflowError("sandbox_policy must be a JSON string")
    value = json.loads(raw)
    keys = {"deny_paths", "read_only_paths"}
    if not isinstance(value, dict) or set(value) - keys:
        raise WorkflowError("sandbox_policy permits only deny_paths and read_only_paths")
    for paths in value.values():
        if paths is not None and (not isinstance(paths, list) or any(not isinstance(p, str) for p in paths)):
            raise WorkflowError("Sandbox paths must be lists of strings")
    return value


def inherit_sandbox(value, workspace):
    profile, policy, seen, has_policy = "", {}, set(), False
    current = value
    while current:
        if current["id"] in seen:
            raise WorkflowError("Issue parent cycle while resolving sandbox")
        seen.add(current["id"])
        props = metadata(current)
        name = props.get("sandbox_profile", "")
        if name:
            if profile and profile != name:
                raise WorkflowError("Issue cannot replace an inherited sandbox profile")
            profile = name
        raw = props.get("sandbox_policy", "")
        if raw:
            has_policy = True
            for key, paths in parse_policy(raw).items():
                for path in paths or []:
                    if path not in policy.setdefault(key, []):
                        policy[key].append(path)
        parent = current.get("parent_issue_id")
        current = issue(parent, workspace) if parent else None
    if profile:
        bind(value, "sandbox_profile", profile)
    if has_policy:
        bind(value, "sandbox_policy", json.dumps({k: v for k, v in policy.items() if v}, separators=(",", ":")))
    return profile, policy if has_policy else None


def create_sandbox_profile(profiles_root, name, cgroup):
    """Register a cgroup launcher in the trusted runtime-owned profile directory."""
    if not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}", name):
        raise WorkflowError("Invalid sandbox profile name")
    group = Path(cgroup).resolve()
    if group == Path('/sys/fs/cgroup') or not group.is_relative_to('/sys/fs/cgroup'):
        raise WorkflowError("Sandbox profile requires a dedicated cgroup")
    command = ['/usr/bin/bash', '-c',
               'if ! /usr/bin/grep -Fxq "0::${1#/sys/fs/cgroup}" /proc/self/cgroup; then '
               'printf %s "$$" | sudo -n tee "$1/cgroup.procs" >/dev/null || exit; fi; '
               'shift 2; exec "$@"', 'multica-cgroup', str(group), '--']
    target = Path(profiles_root) / (name + '.json')
    target.parent.mkdir(parents=True, exist_ok=True)
    temporary = target.with_suffix('.' + str(os.getpid()) + '.tmp')
    temporary.write_text(json.dumps({'command': command}) + '\n')
    temporary.replace(target)
    return name


def sandbox_prefix(profile, policy, workdir, profiles_root=None):
    if not profile and policy is None:
        return []
    if not sys.platform.startswith("linux"):
        raise WorkflowError("Issue sandbox profiles require Linux")
    prefix = []
    if profile:
        if not isinstance(profile, str) or not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}", profile):
            raise WorkflowError("Invalid sandbox profile name")
        if profiles_root is None:
            raise WorkflowError("Set --profiles-root to the trusted runtime-owned .sandboxes directory")
        config = json.loads((profiles_root / (profile + ".json")).read_text())
        prefix = config["command"]
        if (not isinstance(prefix, list) or len(prefix) < 2
                or any(not isinstance(arg, str) for arg in prefix)
                or not Path(prefix[0]).is_absolute() or prefix[-1] != "--"
                or not os.access(prefix[0], os.X_OK)):
            raise WorkflowError("Profile requires an absolute executable and a final --")
    if policy is not None:
        bwrap = ensure_command("bwrap", "bubblewrap")
        prefix += [bwrap, "--die-with-parent", "--unshare-user", "--uid", str(os.getuid()),
                   "--gid", str(os.getgid()), "--unshare-pid", "--unshare-ipc", "--unshare-uts",
                   "--cap-drop", "ALL", "--new-session", "--bind", "/", "/", "--proc", "/proc",
                   "--dev", "/dev", "--ro-bind", "/sys", "/sys"]
        for key in ("read_only_paths", "deny_paths"):
            paths = []
            for raw in policy.get(key, []):
                path = Path(os.path.expanduser(raw) if raw.startswith("~/") else raw)
                if not path.is_absolute() or path.resolve() == Path("/"):
                    raise WorkflowError(f"Sandbox path must be absolute or ~/ and cannot be /: {raw!r}")
                try:
                    path = path.resolve(strict=True)
                except FileNotFoundError:
                    continue
                paths.append(path)
            # Mount parents before children: a read-only child cannot reopen a denied parent.
            for path in sorted(set(paths), key=lambda p: len(p.parts)):
                if key == "read_only_paths":
                    prefix += ["--ro-bind", str(path), str(path)]
                elif path.is_dir():
                    prefix += ["--tmpfs", str(path), "--remount-ro", str(path)]
                else:
                    prefix += ["--ro-bind", "/dev/null", str(path)]
        prefix += ["--chdir", str(workdir), "--"]
    return prefix


def prepare(repo, value, workspace, seen=None):
    seen = set() if seen is None else seen
    if value["id"] in seen:
        raise WorkflowError("Issue parent cycle while preparing branches")
    seen.add(value["id"])
    base = metadata(value).get("git_parent_branch", "")
    if not base and value.get("parent_issue_id"):
        parent = issue(value["parent_issue_id"], workspace)
        base = prepare(repo, parent, workspace, seen)["branch"]
    branch = reserve(repo, value, workspace, base or "main")
    bind_repository(repo, value)
    profile, policy = inherit_sandbox(value, workspace)
    seen.remove(value["id"])
    return {"issue_id": value["id"], "branch": branch, "base_branch": base or "main",
            "sandbox_profile": profile, "sandbox_policy": policy}


def worktree(repo, common, binding, workspace):
    path = checkout(repo, binding["branch"])
    if path is not None:
        return path
    path = common / "multica-sop" / "worktrees" / binding["branch"]
    path.parent.mkdir(parents=True, exist_ok=True)
    git(repo, "worktree", "add", str(path), binding["branch"])
    return path


def bootstrap(path, value, workspace):
    """Bind a supplied checkout through its Git identity before assignment."""
    repo, common = repository(path)
    current = git(repo, "symbolic-ref", "--short", "HEAD").stdout.strip()
    props = metadata(value)
    base = props.get("git_parent_branch") or current
    branch = props.get("git_branch")
    if branch == current and git(repo, "rev-parse", "--verify", "--quiet",
                                 "refs/multica/local-state/" + branch, check=False).returncode:
        record_branch(repo, branch, git(repo, "rev-parse", "HEAD").stdout.strip(), value, workspace)
    branch = reserve(repo, value, workspace, base)
    bind_repository(repo, value)
    target = worktree(repo, common, {"issue_id": value["id"], "branch": branch}, workspace)
    return {"repository": metadata(value).get("git_repository"), "workdir": str(target),
            "branch": branch, "base_branch": base, "tag": "dev"}


def create_child(args, parent, workspace):
    spec = json.loads(args.spec_file.read_text(encoding="utf-8"))
    allowed = {"title", "description_file", "priority", "stage", "project", "due_date", "assignee_id"}
    if not isinstance(spec, dict) or set(spec) - allowed or not isinstance(spec.get("title"), str) or not spec["title"].strip():
        raise WorkflowError("Create spec requires title; see SKILL.md for allowed fields")
    if args.start and not spec.get("assignee_id"):
        raise WorkflowError("--start requires an assignee_id in the spec")
    argv = ["create", "--parent", parent["id"], "--title", spec["title"], "--status", "backlog"]
    for key in ("description_file", "priority", "stage", "project", "due_date"):
        content = spec.get(key)
        if content is not None:
            if key == "description_file":
                path = Path(content).resolve()
                path.read_text(encoding="utf-8")
                content = str(path)
                argv.append("--allow-external-file")
            argv += ["--" + key.replace("_", "-"), str(content)]
    if not spec.get("project") and parent.get("project_id"):
        argv += ["--project", parent["project_id"]]
    # Create once, unassigned and parked; branch/policy binding precedes dispatch.
    value = cli(*argv, workspace=workspace)
    if not isinstance(value, dict) or not value.get("id"):
        raise WorkflowError("Create response lacks id; inspect children before any further create")
    return value, spec.get("assignee_id")


if __name__ == '__main__':
    import argparse
    parser = argparse.ArgumentParser(description='Bind a supplied Git branch before issue assignment.')
    parser.add_argument('--issue', required=True)
    parser.add_argument('--repo', type=Path, required=True)
    args = parser.parse_args()
    workspace = os.environ['MULTICA_WORKSPACE_ID']
    print(json.dumps(bootstrap(args.repo, issue(args.issue, workspace), workspace)))
