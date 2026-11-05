#!/usr/bin/env python3
"""Standard-library helpers bundled into each hosted executable."""
import hashlib
import json
import os
import sys
from urllib.error import HTTPError
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen


def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()


def category(issue):
    return {"done": "done", "closed": "cancelled"}.get(issue.get("status_category"), issue["status"])


def finished(issue):
    return category(issue) in ("done", "cancelled")


class API:
    def __init__(self, payload):
        self.payload = payload
        self.base = os.environ["MULTICA_SERVER_URL"].rstrip("/")
        self.workspace = os.environ["MULTICA_WORKSPACE_ID"]
        if self.workspace != payload["workspace_id"]:
            raise ValueError("workspace mismatch")
        self.installation = payload["installation_id"]
        self.headers = {
            "Authorization": "Bearer " + os.environ["MULTICA_TOKEN"],
            "X-Workspace-ID": self.workspace,
            "X-Multica-Plugin-Installation": self.installation,
            "Content-Type": "application/json",
        }

    def request(self, method, path, body=None, missing=None, response_headers=None):
        data = None if body is None else json.dumps(body).encode()
        try:
            with urlopen(Request(self.base + path, data, self.headers, method=method), timeout=5) as response:
                if response_headers is not None:
                    response_headers.update(response.headers)
                raw = response.read()
                return json.loads(raw) if raw else None
        except HTTPError as error:
            if error.code == 404 and missing is not None:
                return missing
            # Do not echo proxy response bodies, which can contain credentials.
            if error.code == 403:
                raise PermissionError(f"{method} {path.split('?')[0]}: HTTP 403") from None
            raise RuntimeError(f"{method} {path.split('?')[0]}: HTTP {error.code}") from None

    def issue(self, issue_id):
        issue = self.request("GET", "/api/issues/" + quote(issue_id, safe=""), missing={})
        if issue and issue["workspace_id"] != self.workspace:
            raise ValueError("issue outside plugin workspace")
        return issue

    def issues(self):
        # shortcut: scans must fit the hook timeout; add a cursor if workspaces outgrow one invocation.
        offset = 0
        seen = set()
        while True:
            page = self.request("GET", "/api/issues?" + urlencode({"limit": 100, "offset": offset}))
            rows = page["issues"]
            if not rows:
                return
            for row in rows:
                if row["id"] in seen or row["workspace_id"] != self.workspace:
                    raise ValueError("invalid issue page")
                seen.add(row["id"])
                yield row
            offset += len(rows)

    def children(self, issue_id):
        return self.request("GET", f"/api/issues/{quote(issue_id, safe='')}/children")["issues"]

    def runs(self, issue_id, active=False):
        # The API's active filter excludes deferred retries, which also block a new run.
        rows = self.request("GET", f"/api/issues/{quote(issue_id, safe='')}/task-runs")
        return [row for row in rows if row["status"] in ("queued", "deferred", "running", "dispatched", "waiting_local_directory")] if active else rows

    def state(self, key, value=None):
        path = "/api/plugin-bridge/v1/storage/workspace/" + quote(key, safe="")
        if value is None:
            row = self.request("GET", path, missing={"value": "{}"})
            return json.loads(row["value"])
        self.request("PUT", path, {"value": json.dumps(value)})

    def continuation_thread(self, issue_id, task_id, prompt, receipt):
        marker = "<!-- multica-ralph:" + self.installation + ":" + task_id + " -->"
        headers = {}
        rows = self.request("GET", f"/api/issues/{issue_id}/comments?roots_only=true", response_headers=headers)
        if {key.lower(): value for key, value in headers.items()}.get("x-comments-truncated") == "true":
            raise RuntimeError("continuation comment inventory is truncated")
        matches = [row for row in rows if marker in row.get("content", "")]
        if not matches:
            if receipt.get("pending_run") == task_id:
                raise RuntimeError("continuation comment creation is uncertain; inspect the issue before retrying")
            token = self.payload.get("callback_token")
            callback_url = self.payload.get("callback_url")
            if not token or not callback_url:
                raise RuntimeError("continuation requires the event hook callback URL and token")
            self.state("issue:" + issue_id, dict(receipt, pending_run=task_id))
            request = Request(callback_url.rstrip("/") + f"/issues/{issue_id}/comments",
                              json.dumps({"content": marker + "\n\n" + prompt}).encode(),
                              {**self.headers, "Authorization": "Bearer " + token}, method="POST")
            with urlopen(request, timeout=5) as response:
                matches = [json.load(response)]
        if (len(matches) != 1 or matches[0].get("author_type") != "plugin"
                or matches[0].get("author_id") != self.installation or not matches[0].get("id")
                or matches[0].get("parent_id") or matches[0].get("content") != marker + "\n\n" + prompt):
            raise RuntimeError("continuation comment does not match this plugin and completed run")
        return matches[0]["id"]

    def target(self, issue):
        if issue.get("assignee_type") == "agent":
            return issue.get("assignee_id")
        if issue.get("assignee_type") == "squad":
            squad = self.request("GET", "/api/squads/" + quote(issue["assignee_id"], safe=""), missing={})
            if not squad.get("archived_at"):
                return squad.get("leader_id")
        return None

    def wake(self, issue, key, prompt, parent_comment_id=None):
        # Re-read before writing: an event may describe an obsolete assignment.
        current = self.issue(issue["id"])
        if not current or finished(current) or category(current) == "backlog":
            return False
        agent = self.target(current)
        if not agent:
            return False
        try:
            self.request("POST", f"/api/issues/{current['id']}/wakeups", {
                "agent_id": agent, "instruction": prompt, "kind": "at", "after_seconds": 1,
                "idempotency_key": fingerprint([self.installation, key, agent]),
                **({"parent_comment_id": parent_comment_id} if parent_comment_id else {}),
            })
        except PermissionError:
            return False
        return True


def run(main):
    try:
        payload = json.load(sys.stdin)
        print(json.dumps(main(API(payload), payload)))
    except Exception as error:
        print(json.dumps({"error": str(error)}))
        sys.exit(1)
