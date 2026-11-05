#!/usr/bin/env python3
"""Install and back up workspace harnesses using the Multica API."""
import argparse
import copy
import io
import json
import os
from pathlib import Path
import sys
from urllib.error import HTTPError
from urllib.request import Request, urlopen
from zipfile import ZIP_DEFLATED, ZipFile


SKILL = Path(__file__).resolve().parents[1]
EMBEDDED_KIT = "assets/startup-kit.json"
SKILL_FIELDS = ("name", "description", "content", "config", "is_system_skill")
AGENT_FIELDS = ("name", "description", "instructions", "model", "runtime_id", "runtime_config",
                "thinking_level", "service_tier", "custom_args", "mcp_config", "visibility",
                "permission_mode", "invocation_targets", "max_concurrent_tasks", "conversation_starters")


class API:
    def __init__(self):
        self.base = os.environ["MULTICA_SERVER_URL"].rstrip("/")
        self.workspace = os.environ["MULTICA_WORKSPACE_ID"]
        self.token = os.environ["MULTICA_TOKEN"]
        self.headers = {"Authorization": "Bearer " + self.token,
                        "X-Workspace-ID": self.workspace}

    def request(self, method, path, body=None, *, bundle=None):
        headers = dict(self.headers)
        data = None
        if bundle is not None:
            boundary = "multica-harness-bundle"
            headers["Content-Type"] = "multipart/form-data; boundary=" + boundary
            data = (f'--{boundary}\r\nContent-Disposition: form-data; name="bundle"; filename="plugin.zip"\r\n'
                    'Content-Type: application/zip\r\n\r\n').encode() + bundle + f"\r\n--{boundary}--\r\n".encode()
        elif body is not None:
            headers["Content-Type"] = "application/json"
            data = json.dumps(body).encode()
        try:
            with urlopen(Request(self.base + path, data=data, headers=headers, method=method), timeout=60) as response:
                raw = response.read()
                return json.loads(raw) if raw else None
        except HTTPError as error:
            message = error.read().decode(errors="replace")
            raise RuntimeError(f"{method} {path}: HTTP {error.code}: {message}") from None


def rows(value, key):
    return value if isinstance(value, list) else value[key]


def skill_body(skill):
    body = {k: skill[k] for k in SKILL_FIELDS if k in skill}
    body["files"] = [{"path": f["path"], "content": f["content"]} for f in skill.get("files", [])]
    return body


def load_skill(directory, system):
    content = (directory / "SKILL.md").read_text()
    # Only name and description are package metadata; all other frontmatter stays in SKILL.md.
    front = content.split("---", 2)[1]
    metadata = dict(line.split(":", 1) for line in front.splitlines() if ":" in line and not line.startswith(" "))
    description = metadata["description"].strip()
    if description.startswith('"'):
        description = json.loads(description)
    elif description.startswith("'"):
        description = description[1:-1].replace("''", "'")
    elif description in {">", "|", ">-", "|-"}:
        lines = front.splitlines(); start = next(i for i, line in enumerate(lines) if line.startswith("description:")) + 1
        parts = []
        for line in lines[start:]:
            if line and not line.startswith(" "):
                break
            parts.append(line.strip())
        description = " ".join(parts).strip()
    files = [{"path": str(p.relative_to(directory)), "content": p.read_text()}
             for p in sorted(directory.rglob("*")) if p.is_file() and p != directory / "SKILL.md"
             and "__pycache__" not in p.parts and p.suffix != ".pyc" and str(p.relative_to(directory)) != EMBEDDED_KIT]
    return {"name": metadata["name"].strip(), "description": description, "content": content,
            "config": {}, "is_system_skill": system, "files": files}


def load_kit(directory=None):
    if directory is None:
        return json.loads((SKILL / EMBEDDED_KIT).read_text())
    directory = Path(directory)
    manifest = json.loads((directory / "manifest.json").read_text())
    skills = [load_skill(directory / "skills" / name, name in manifest["system_skills"])
              for name in manifest["system_skills"] + manifest["skills"]]
    plugins = []
    for name in manifest["plugins"]:
        source = directory / "plugins" / name
        plugin = json.loads((source / "multica.plugin.json").read_text())
        plugins.append({"key": plugin["key"], "files": {
            "multica.plugin.json": json.dumps(plugin),
            "hook.py": (source.parent / "runtime.py").read_text() + "\n" + (source / "hook.py").read_text()},
            "config": manifest["plugin_config"].get(name, {}), "enabled": True})
    return {"context": (directory / "startup.gotmpl").read_text(), "skills": skills, "plugins": plugins}


def install_skills(api, skills):
    existing = rows(api.request("GET", "/api/skills"), "skills")
    by_id = {s["id"]: s for s in existing}
    by_name = {s["name"]: s for s in existing}
    bindings = {}
    for skill in sorted(skills, key=lambda s: not s.get("is_system_skill", False)):
        previous = by_id.get(skill.get("id")) or by_name.get(skill["name"])
        path = "/api/skills" + ("/" + previous["id"] if previous else "")
        saved = api.request("PUT" if previous else "POST", path, skill_body(skill))
        bindings[skill.get("id", skill["name"])] = saved["id"]
    return bindings


def plugin_bundle(files):
    output = io.BytesIO()
    with ZipFile(output, "w", ZIP_DEFLATED) as archive:
        for name, content in files.items():
            archive.writestr(name, content)
    return output.getvalue()


def configure_execution(api):
    path = f"/api/workspaces/{api.workspace}/plugins/execution"
    if api.request("GET", path)["enabled"]:
        return
    token = api.token
    if not token.startswith("mul_"):
        token = api.request("POST", "/api/tokens", {"name": "Workspace plugins: " + api.workspace})["token"]
    api.request("PUT", path, {"token": token})


def install_plugins(api, plugins):
    if not plugins:
        return
    path = f"/api/workspaces/{api.workspace}/plugins"
    existing = rows(api.request("GET", path), "plugins")
    for plugin in plugins:
        version = plugin.get("package_version_id")
        if plugin.get("files"):
            manifest = json.loads(plugin["files"]["multica.plugin.json"])
            packages = rows(api.request("GET", path + "/packages"), "packages")
            versions = [v for p in packages if p["plugin_key"] == manifest["key"]
                        for v in p["versions"] if v["version"] == manifest["version"]]
            if versions:
                version = versions[0]["id"]
            else:
                published = api.request("POST", path + "/packages", bundle=plugin_bundle(plugin["files"]))
                version = next(v["id"] for v in published["versions"] if v["version"] == manifest["version"])
        preview = api.request("POST", path + "/preview", {"version_id": version})
        key = preview["manifest"]["key"]
        current = next((p for p in existing if p["plugin_key"] == key), None)
        installed = current if current and current["package_version_id"] == version else api.request(
            "POST", path, {"version_id": version, "granted_scopes": preview["scopes"]})
        target = path + "/" + installed["id"]
        api.request("PUT", target + "/config", {"values": plugin.get("config", {})})
        api.request("POST", target + ("/enable" if plugin.get("enabled", True) else "/disable"), {})


def install(api, kit, keep_context=False, plugins=True):
    kit = copy.deepcopy(kit)
    for skill in kit["skills"]:
        if skill["name"] == "harness-backup-restore":
            skill["files"] = [f for f in skill["files"] if f["path"] != EMBEDDED_KIT]
            # Store the clean kit once; installation regenerates this attachment for itself.
            skill["files"].append({"path": EMBEDDED_KIT, "content": json.dumps(copy.deepcopy(kit))})
    if plugins and kit.get("plugins"):
        configure_execution(api)
    install_skills(api, kit["skills"])
    if plugins and kit.get("plugins"):
        install_plugins(api, kit["plugins"])
    if not keep_context:
        api.request("PATCH", "/api/workspaces/" + api.workspace, {"context": kit["context"]})


def backup(api, plugins=True):
    skills = [api.request("GET", "/api/skills/" + s["id"]) for s in rows(api.request("GET", "/api/skills"), "skills")]
    agents = [api.request("GET", "/api/agents/" + a["id"]) for a in rows(api.request("GET", "/api/agents"), "agents")]
    plugins = rows(api.request("GET", f"/api/workspaces/{api.workspace}/plugins"), "plugins") if plugins else []
    workspace = api.request("GET", "/api/workspaces/" + api.workspace)
    agents = [{"id": a["id"], "skills": a.get("skills", []),
               **{k: a[k] for k in AGENT_FIELDS if k in a and not (k == "mcp_config" and a.get("mcp_config_redacted"))}}
              for a in agents]
    return {"schema_version": 1, "server_url": api.base, "workspace_id": api.workspace,
            "context": workspace.get("context"), "skills": skills, "agents": agents, "plugins": plugins}


def restore(api, snapshot):
    if snapshot.get("schema_version") != 1 or (snapshot["server_url"], snapshot["workspace_id"]) != (api.base, api.workspace):
        raise ValueError("Backup belongs to another server or workspace")
    skills = install_skills(api, snapshot["skills"])
    for agent in snapshot["agents"]:
        identity = agent["id"]
        body = {k: agent[k] for k in AGENT_FIELDS if k in agent}
        for key in ("description", "instructions", "model", "thinking_level", "service_tier"):
            if key in body and body[key] is None:
                body[key] = ""
        for key, empty in (("runtime_config", {}), ("custom_args", []), ("conversation_starters", [])):
            if key in body and body[key] is None:
                body[key] = empty
        api.request("PUT", "/api/agents/" + identity, body)
        selected = [skills[s["id"]] for s in agent.get("skills", [])]
        api.request("PUT", "/api/agents/" + identity + "/skills", {"skill_ids": selected})
        for skill in agent.get("skills", []):
            if skill.get("enabled") is False:
                api.request("PUT", f'/api/agents/{identity}/skills/{skills[skill["id"]]}/enabled', {"enabled": False})
    install_plugins(api, snapshot["plugins"])
    api.request("PATCH", "/api/workspaces/" + api.workspace, {"context": snapshot["context"]})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    command = commands.add_parser("install")
    command.add_argument("--kit", type=Path)
    command.add_argument("--keep-context", action="store_true")
    command.add_argument("--without-plugins", action="store_true")
    command = commands.add_parser("backup")
    command.add_argument("--output", type=Path, required=True)
    command.add_argument("--without-plugins", action="store_true")
    commands.add_parser("restore").add_argument("--input", type=Path, required=True)
    args = parser.parse_args()
    api = API()
    if args.command == "install":
        install(api, load_kit(args.kit), args.keep_context, not args.without_plugins)
    elif args.command == "backup":
        with args.output.open("x", opener=lambda path, flags: os.open(path, flags, 0o600)) as output:
            json.dump(backup(api, not args.without_plugins), output, indent=2)
    else:
        restore(api, json.loads(args.input.read_text()))
    print(json.dumps({"status": "complete", "operation": args.command, "workspace_id": api.workspace}))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, KeyError) as error:
        sys.exit(str(error))
