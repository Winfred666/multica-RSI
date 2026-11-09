#!/usr/bin/env python3
"""Install, export or import a harness through the native workspace package API."""
import argparse
import json
import os
from pathlib import Path
import sys
from urllib.error import HTTPError
from urllib.request import Request, urlopen


class API:
    def __init__(self):
        self.base = os.environ["MULTICA_SERVER_URL"].rstrip("/")
        self.workspace = os.environ["MULTICA_WORKSPACE_ID"]
        self.headers = {"Authorization": "Bearer " + os.environ["MULTICA_TOKEN"],
                        "Content-Type": "application/json"}

    def request(self, method, endpoint, body=None):
        data = None if body is None else json.dumps(body).encode()
        url = self.base + "/api/workspaces/" + self.workspace + endpoint
        try:
            with urlopen(Request(url, data=data, headers=self.headers, method=method), timeout=90) as response:
                return json.load(response)
        except HTTPError as error:
            raise RuntimeError(f"{method} {endpoint}: HTTP {error.code}: {error.read().decode()}") from None


def selection(package, *, plugins=True, prompt=True):
    return {"projects_issues": False, "system_prompt": prompt,
            **{key: [item["id"] for item in package[key]]
               for key in ("agents", "skills", "mcps", "plugins")
               if key != "plugins" or plugins}}


def inventory_selection(api, *, plugins=True):
    selected = {"projects_issues": False, "system_prompt": True}
    for category in ("agents", "skills", "mcps", "plugins"):
        selected[category] = []
        if category == "plugins" and not plugins:
            continue
        offset = 0
        while True:
            page = api.request("GET", f"/package?category={category}&limit=100&offset={offset}")
            items = page.get("items", [])
            selected[category].extend(item["id"] for item in items)
            offset += len(items)
            if not items or offset >= page["counts"][category]:
                break
    return selected


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    command = commands.add_parser("install")
    command.add_argument("--keep-context", action="store_true")
    command.add_argument("--without-plugins", action="store_true")
    command = commands.add_parser("backup")
    command.add_argument("--output", type=Path, required=True)
    command.add_argument("--without-plugins", action="store_true")
    command = commands.add_parser("restore")
    command.add_argument("--input", type=Path, required=True)
    args = parser.parse_args()
    api = API()
    if args.command == "install":
        package = api.request("GET", "/package/default")
        api.request("POST", "/import", {"package": package, "selection": selection(
            package, plugins=not args.without_plugins, prompt=not args.keep_context)})
    elif args.command == "backup":
        package = api.request("POST", "/export", inventory_selection(api, plugins=not args.without_plugins))
        with open(args.output, "x", opener=lambda path, flags: os.open(path, flags, 0o600)) as output:
            json.dump(package, output, indent=2)
    else:
        api.request("POST", "/import", {"package": json.loads(args.input.read_text())})
    print(json.dumps({"status": "complete", "operation": args.command, "workspace_id": api.workspace}))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, KeyError) as error:
        sys.exit(str(error))
