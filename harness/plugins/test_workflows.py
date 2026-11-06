import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, unquote, urlsplit
from zipfile import ZipFile

import runtime
from package import package


ROOT = Path(__file__).parent


def policy(name):
    namespace = dict(vars(runtime), __name__="test_policy")
    exec(compile((ROOT / name / "hook.py").read_text(), name, "exec"), namespace)
    return namespace["main"]


CHILD = policy("wakeup-for-sub-issue")


class FakeAPI(runtime.API):
    def __init__(self):
        self.workspace = "workspace"
        self.installation = "installation"
        self.rows = {
            "parent": self.row("parent", "in_progress"),
            "child": self.row("child", "todo", parent="parent"),
        }
        self.storage = {}
        self.tasks = {}
        self.rules = {}
        self.writes = []
        self.fail_storage = False
        self.denied = set()
        self.timeline = {}
        self.timeline_headers = {}
        self.comments = {}

    @staticmethod
    def row(issue_id, status, parent=None):
        return {"id": issue_id, "workspace_id": "workspace", "status": status, "status_category": status,
                "parent_issue_id": parent, "assignee_type": "agent", "assignee_id": "agent",
                "updated_at": "2026-01-01T00:00:00Z"}

    def status(self, issue, status):
        old = self.rows[issue]["status"]
        self.rows[issue]["status"] = status
        self.rows[issue]["status_category"] = status
        self.rows[issue]["updated_at"] += "1"
        rows = self.timeline.setdefault(issue, [])
        rows.append({"id": str(len(rows)), "action": "status_changed", "details": {"from": old, "to": status}})

    def request(self, method, path, body=None, missing=None, response_headers=None):
        parsed = urlsplit(path)
        parts = parsed.path.split("/")
        if parsed.path == "/api/issues":
            query = parse_qs(parsed.query)
            offset = int(query["offset"][0])
            return {"issues": list(self.rows.values())[offset:offset + 100]}
        if "/storage/workspace/" in path:
            key = unquote(parts[-1])
            if method == "GET":
                return {"value": json.dumps(self.storage.get(key, {}))}
            if self.fail_storage:
                self.fail_storage = False
                raise RuntimeError("lost storage response")
            self.storage[key] = json.loads(body["value"])
            return None
        issue_id = parts[3]
        if len(parts) == 4:
            return copy.deepcopy(self.rows.get(issue_id, missing))
        if parts[4] == "children":
            return {"issues": [copy.deepcopy(row) for row in self.rows.values() if row.get("parent_issue_id") == issue_id]}
        if parts[4] == "task-runs":
            return self.tasks.get(issue_id, [])
        if parts[4] == "timeline":
            if response_headers is not None:
                response_headers.update(self.timeline_headers.get(issue_id, {}))
            return self.timeline.get(issue_id, [])
        if parts[4] == "comments" and method == "GET":
            query = parse_qs(parsed.query)
            if "tail" in query and "thread" not in query:
                raise ValueError("tail requires thread")
            return self.comments.get(issue_id, [])
        if parts[4] == "comments" and method == "POST":
            self.writes.append(dict(body, issue_id=issue_id))
            return self.writes[-1]
        if parts[4] == "wakeups":
            if method == "GET":
                return [row for row in self.rules.values() if row["issue_id"] == issue_id]
            if issue_id in self.denied:
                raise PermissionError("not invocable")
            key = body["idempotency_key"]
            if key not in self.rules:
                self.rules[key] = dict(body, issue_id=issue_id, enabled=True)
                self.writes.append(self.rules[key])
            return self.rules[key]
        raise AssertionError((method, path))


class ChildWakeupTests(unittest.TestCase):
    def setUp(self):
        self.api = FakeAPI()

    def check(self, hook, event=True, prompt="Configured instruction"):
        return CHILD(self.api, {"hook_key": hook, "issue_id": "child" if event else "",
                                "config": {hook + "_prompt": prompt}})

    def test_three_hooks_and_duplicate_events(self):
        self.check("all_finished")
        self.assertEqual(self.api.writes, [])
        for status, hook in (("in_review", "in_review"), ("blocked", "blocked"), ("done", "all_finished")):
            self.api.status("child", status)
            self.check(hook)
            self.check(hook, event=False)
        self.assertEqual(len(self.api.writes), 3)
        self.assertTrue(all(row["instruction"].endswith("Configured instruction") for row in self.api.writes))

    def test_parent_steering_contains_child_reply_and_git_binding(self):
        self.api.rows['child']['metadata'] = {'git_branch': 'issue/child', 'git_parent_branch': 'issue/parent'}
        self.api.comments['child'] = [{'id': 'reply', 'author_type': 'agent', 'author_id': 'agent',
                                      'content': 'Score: 0.42. Improved model. Commit: abc123.'}]
        self.api.status('child', 'in_review')
        self.check('in_review')
        prompt = self.api.writes[0]['instruction']
        self.assertIn('issue/child', prompt)
        self.assertIn('issue/parent', prompt)
        self.assertIn('Score: 0.42', prompt)
        self.assertIn('Commit: abc123', prompt)
        self.assertEqual(set(self.api.rows['child']['metadata']), {'git_branch', 'git_parent_branch'})

    def test_rework_and_new_children_rearm(self):
        for status in ("in_review", "in_progress", "in_review"):
            self.api.status("child", status)
            self.check("in_review")
        self.assertEqual(len(self.api.writes), 2)
        self.api.status("child", "cancelled")
        self.check("all_finished")
        self.api.rows["second"] = self.api.row("second", "todo", "parent")
        self.check("all_finished")
        self.api.status("second", "done")
        self.check("all_finished")
        self.assertEqual(len(self.api.writes), 4)
        self.api.rows["third"] = self.api.row("third", "done", "parent")
        self.check("all_finished")
        self.check("all_finished")
        self.assertEqual(len(self.api.writes), 5)

    def test_busy_parent_gets_one_durable_wakeup_without_polling(self):
        for status in ("queued", "deferred", "running", "dispatched", "waiting_local_directory"):
            with self.subTest(status=status):
                self.api = FakeAPI()
                self.api.status("child", "blocked")
                self.api.tasks["parent"] = [{"status": status}]
                with patch.object(self.api, "issues", side_effect=AssertionError("workspace scan")), \
                     patch.object(self.api, "runs", side_effect=AssertionError("busy-parent check")):
                    self.check("blocked")
                    self.check("blocked")
                self.assertEqual(len(self.api.writes), 1)
                self.assertEqual(self.api.writes[0]["kind"], "at")
                self.assertEqual(self.api.writes[0]["after_seconds"], 1)
                self.assertTrue(self.api.writes[0]["instruction"].endswith("Configured instruction"))

    def test_unobserved_departure_and_return_delivers_new_review(self):
        self.api.status("child", "in_review")
        self.check("in_review")
        self.api.status("child", "in_progress")
        self.api.status("child", "in_review")
        self.check("in_review")
        self.check("in_review", event=False)
        self.assertEqual(len(self.api.writes), 2)
        self.api.rows["child"]["updated_at"] += "metadata edit"
        self.check("in_review")
        self.assertEqual(len(self.api.writes), 2)

    def test_unobserved_reopen_and_finish_rearms_all_finished(self):
        self.api.status("child", "done")
        self.check("all_finished")
        self.api.status("child", "in_progress")
        self.api.status("child", "done")
        self.check("all_finished")
        self.check("all_finished")
        self.assertEqual(len(self.api.writes), 2)

    def test_truncated_history_does_not_rearm_delivered_transitions(self):
        for status, hook in (("in_review", "in_review"), ("blocked", "blocked"), ("done", "all_finished")):
            with self.subTest(hook=hook):
                self.api = FakeAPI()
                self.api.status("child", status)
                self.check(hook)
                self.api.timeline["child"] = []
                self.api.timeline_headers["child"] = {"X-Timeline-Truncated": "activity"}
                self.check(hook)
                self.check(hook, event=False)
                self.assertEqual(len(self.api.writes), 1)
                self.api.status("child", "in_progress")
                self.api.status("child", status)
                self.check(hook)
                self.check(hook)
                self.assertEqual(len(self.api.writes), 2)

    def test_new_finished_child_rearms_with_truncated_sibling_history(self):
        self.api.status("child", "done")
        self.check("all_finished")
        self.api.timeline["child"] = []
        self.api.timeline_headers["child"] = {"X-Timeline-Truncated": "activity"}
        self.api.rows["second"] = self.api.row("second", "done", "parent")
        self.check("all_finished")
        self.check("all_finished")
        self.assertEqual(len(self.api.writes), 2)

    def test_truncated_history_after_state_failure_does_not_duplicate(self):
        self.api.status("child", "in_review")
        self.api.fail_storage = True
        with self.assertRaises(RuntimeError):
            self.check("in_review")
        self.api.timeline["child"] = []
        self.api.timeline_headers["child"] = {"X-Timeline-Truncated": "activity"}
        self.check("in_review")
        self.assertEqual(len(self.api.writes), 1)

    def test_uninvocable_parent_does_not_stop_other_parents(self):
        self.api.status("child", "blocked")
        self.api.denied.add("parent")
        self.api.rows["second"] = self.api.row("second", "in_progress")
        self.api.rows["second-child"] = self.api.row("second-child", "blocked", "second")
        self.check("blocked", event=False)
        self.assertEqual([row["issue_id"] for row in self.api.writes], ["second"])

    def test_retry_after_state_write_failure_does_not_duplicate(self):
        self.api.status("child", "in_review")
        self.api.fail_storage = True
        with self.assertRaises(RuntimeError):
            self.check("in_review")
        self.check("in_review")
        self.assertEqual(len(self.api.writes), 1)

    def test_closed_parked_human_and_detached_parents_do_not_run(self):
        self.api.status("child", "in_review")
        for status in ("done", "cancelled", "backlog"):
            self.api.status("parent", status)
            self.check("in_review")
        self.api.status("parent", "in_progress")
        self.api.rows["parent"]["assignee_type"] = "member"
        self.check("in_review")
        self.api.rows["child"]["parent_issue_id"] = None
        self.check("in_review")
        self.assertEqual(self.api.writes, [])

    def test_custom_terminal_status_and_no_children(self):
        self.api.rows["child"]["status"] = "shipped"
        self.api.rows["child"]["status_category"] = "done"
        self.check("all_finished")
        self.assertEqual(len(self.api.writes), 1)
        self.api.rows.pop("child")
        self.assertEqual(self.check("all_finished", event=False)["wakeups"], 0)






class PackagingTests(unittest.TestCase):
    def test_child_package_is_executable(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "child.zip"
            package(ROOT / "wakeup-for-sub-issue", output)
            with ZipFile(output) as bundle:
                self.assertEqual(set(bundle.namelist()), {"multica.plugin.json", "hook.py"})
                compile(bundle.read("hook.py"), "hook.py", "exec")
                manifest = json.loads(bundle.read("multica.plugin.json"))
                for hook in manifest["contributes"]["hooks"]:
                    self.assertEqual(hook["events"], ["issue.created", "issue.updated", "issue.status_changed"])


if __name__ == "__main__":
    unittest.main()
