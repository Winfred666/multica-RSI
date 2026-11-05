import copy
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
from urllib.parse import unquote, urlsplit
from zipfile import ZipFile

ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))
import runtime
from package import package


def policy(name):
    namespace = dict(vars(runtime), __name__="test_policy")
    exec(compile((ROOT / name / "hook.py").read_text(), name, "exec"), namespace)
    return namespace["main"]


RALPH = policy("in-progress-ralph")


class FakeAPI(runtime.API):
    def __init__(self):
        self.workspace = "workspace"
        self.installation = "installation"
        self.base = "https://multica.invalid"
        self.headers = {"Authorization": "Bearer member-token"}
        self.payload = {"callback_token": "event-callback", "callback_url": "https://plugin-api.invalid/v1/"}
        self.rows = {
            "parent": self.row("parent", "in_progress"),
            "child": self.row("child", "todo", parent="parent"),
        }
        self.storage = {}
        self.tasks = {}
        self.rules = {}
        self.writes = []
        self.comment_posts = 0
        self.failure = None
        self.truncated = False

    @staticmethod
    def row(issue_id, status, parent=None):
        group = {'backlog': 'unstarted', 'todo': 'unstarted', 'in_progress': 'started',
                 'in_review': 'started', 'blocked': 'started', 'done': 'done', 'cancelled': 'closed'}[status]
        return {"id": issue_id, "workspace_id": "workspace", "status": status, "status_category": group,
                "parent_issue_id": parent, "assignee_type": "agent", "assignee_id": "agent",
                "updated_at": "2026-01-01T00:00:00Z", "project_id": "trial-project"}

    def status(self, issue, status):
        self.rows[issue]["status"] = status
        self.rows[issue]["status_category"] = self.row(issue, status)['status_category']
        self.rows[issue]["updated_at"] += "1"

    def request(self, method, path, body=None, missing=None, response_headers=None):
        parsed = urlsplit(path)
        parts = parsed.path.split("/")
        if "/storage/workspace/" in path:
            key = unquote(parts[-1])
            if method == "GET":
                return {"value": json.dumps(self.storage.get(key, {}))}
            self.storage[key] = json.loads(body["value"])
            return None
        issue_id = parts[3]
        if len(parts) == 4:
            return copy.deepcopy(self.rows.get(issue_id, missing))
        if parts[4] == "children":
            return {"issues": [copy.deepcopy(row) for row in self.rows.values() if row.get("parent_issue_id") == issue_id]}
        if parts[4] == "task-runs":
            return self.tasks.get(issue_id, [])
        if parts[4] == "comments" and method == "GET":
            if self.truncated:
                response_headers["X-Comments-Truncated"] = "true"
            return copy.deepcopy(self.writes)
        if parts[4] == "wakeups" and method == "GET":
            return [row for row in self.rules.values() if row["issue_id"] == issue_id]
        if parts[4] == "wakeups" and method == "POST":
            key = body["idempotency_key"]
            rule = self.rules.setdefault(key, dict(body, id=key, issue_id=issue_id, enabled=True))
            if self.failure == "wakeup_response":
                self.failure = None
                raise TimeoutError("lost wakeup response")
            return copy.deepcopy(rule)
        raise AssertionError((method, path))

    def callback(self, request, timeout):
        assert request.get_header("Authorization") == "Bearer event-callback"
        assert request.full_url == "https://plugin-api.invalid/v1/issues/parent/comments"
        assert request.get_method() == "POST"
        self.comment_posts += 1
        if self.failure == "comment_not_saved":
            self.failure = None
            raise TimeoutError("uncertain comment write")
        row = dict(json.loads(request.data), id="comment-" + str(self.comment_posts),
                   author_type="plugin", author_id=self.installation, parent_id=None)
        self.writes.append(row)
        if self.failure == "comment_response":
            self.failure = None
            raise TimeoutError("lost comment response")
        return io.BytesIO(json.dumps(row).encode())


class RalphTests(unittest.TestCase):
    def setUp(self):
        self.api = FakeAPI()
        self.api.rows.pop("child")
        self.api.tasks["parent"] = [{"id": "run-1", "status": "completed", "created_at": "2026-01-01T00:00:00Z"}]
        self.payload = {"event_type": "task.completed", "issue_id": "parent", "input": {"task_id": "run-1"},
                        "config": {"prompt": "Continue the task"}}
        callback = patch.object(runtime, "urlopen", side_effect=self.api.callback)
        callback.start()
        self.addCleanup(callback.stop)

    def test_continues_in_plugin_thread_once_per_completed_run(self):
        self.assertEqual(RALPH(self.api, self.payload), {"continued": 1})
        self.assertEqual(self.api.writes[0]["content"], "<!-- multica-ralph:installation:run-1 -->\n\nContinue the task")
        rule = next(iter(self.api.rules.values()))
        self.assertEqual((rule["parent_comment_id"], rule["agent_id"], rule["instruction"], rule["after_seconds"]),
                         ("comment-1", "agent", "Continue the task", 1))
        RALPH(self.api, self.payload)
        self.assertEqual(len(self.api.writes), 1)
        rule["enabled"] = False
        self.api.tasks["parent"][0]["id"] = "run-2"
        self.payload["input"]["task_id"] = "run-2"
        RALPH(self.api, self.payload)
        self.assertEqual(len(self.api.writes), 2)
        self.assertEqual(len(self.api.rules), 2)

    def test_lost_comment_and_wakeup_responses_reuse_native_records(self):
        self.api.failure = "comment_response"
        with self.assertRaises(TimeoutError):
            RALPH(self.api, self.payload)
        self.api.failure = "wakeup_response"
        with self.assertRaises(TimeoutError):
            RALPH(self.api, self.payload)
        # A consumed one-shot is still the receipt; retry must not rearm it.
        rule = next(iter(self.api.rules.values()))
        rule["enabled"] = False
        self.assertEqual(RALPH(self.api, self.payload), {"continued": 1})
        self.assertFalse(rule["enabled"])
        self.assertEqual((self.api.comment_posts, len(self.api.writes), len(self.api.rules)), (1, 1, 1))
        self.assertEqual(self.api.storage["issue:parent"], {"last_run": "run-1"})

    def test_uncertain_or_untrusted_comment_readback_cannot_publish_again(self):
        callback_url = self.api.payload.pop("callback_url")
        with self.assertRaisesRegex(RuntimeError, "callback URL and token"):
            RALPH(self.api, self.payload)
        self.assertEqual(self.api.storage, {})
        self.api.payload["callback_url"] = callback_url
        self.api.failure = "comment_not_saved"
        with self.assertRaises(TimeoutError):
            RALPH(self.api, self.payload)
        with self.assertRaisesRegex(RuntimeError, "uncertain"):
            RALPH(self.api, self.payload)
        self.api.truncated = True
        with self.assertRaisesRegex(RuntimeError, "truncated"):
            RALPH(self.api, self.payload)
        self.api.truncated = False
        self.api.writes.append({"id": "forged", "author_type": "member", "author_id": "owner",
            "content": "<!-- multica-ralph:installation:run-1 -->\n\nContinue the task"})
        with self.assertRaisesRegex(RuntimeError, "does not match"):
            RALPH(self.api, self.payload)
        self.assertEqual(self.api.comment_posts, 1)
        self.assertEqual(self.api.rules, {})

    def test_respects_human_handoffs_active_runs_and_failures(self):
        for state in ("in_review", "blocked", "done", "cancelled", "backlog", "todo"):
            self.api.status("parent", state)
            RALPH(self.api, self.payload)
        self.api.status("parent", "in_progress")
        for state in ("queued", "running", "dispatched", "deferred", "waiting_local_directory", "failed", "cancelled"):
            self.api.tasks["parent"][0]["status"] = state
            RALPH(self.api, self.payload)
        self.assertEqual(self.api.writes, [])

    def test_waits_for_children_and_rules(self):
        self.api.rows["child"] = self.api.row("child", "todo", "parent")
        self.api.tasks["child"] = [{"id": "child-run", "status": "queued"}]
        RALPH(self.api, self.payload)
        self.api.rows.pop("child")
        self.api.rules["wait"] = {"issue_id": "parent", "enabled": True}
        RALPH(self.api, self.payload)
        self.assertEqual(self.api.writes, [])

    def test_idle_unstarted_child_does_not_silence_parent(self):
        self.api.rows["child"] = self.api.row("child", "backlog", "parent")
        self.assertEqual(RALPH(self.api, self.payload), {"continued": 1})

    def test_failed_run_continues_once_without_reclassifying_the_error(self):
        self.api.tasks['parent'][0].update(status='failed', error='Request timed out.')
        self.payload['event_type'] = 'task.failed'
        self.assertEqual(RALPH(self.api, self.payload), {'continued': 1})
        self.assertEqual(RALPH(self.api, self.payload), {'continued': 0})
        self.assertEqual(len(self.api.rules), 1)

    def test_ready_child_does_not_silence_idle_parent(self):
        for status in ('in_review', 'blocked'):
            with self.subTest(status=status):
                api = FakeAPI()
                api.status('child', status)
                api.tasks['parent'] = self.api.tasks['parent']
                with patch.object(runtime, 'urlopen', side_effect=api.callback):
                    self.assertEqual(RALPH(api, self.payload), {'continued': 1})

    def test_child_completion_resumes_parent_review_once(self):
        self.api.rows['child'] = self.api.row('child', 'in_review', 'parent')
        self.api.tasks['child'] = [{'id': 'child-run', 'status': 'completed', 'created_at': '2026-01-01T00:01:00Z'}]
        self.payload.update(issue_id='child', input={'task_id': 'child-run'})
        def callback(request, timeout):
            self.assertEqual(request.full_url, 'https://plugin-api.invalid/v1/issues/child/comments')
            self.api.writes.append({'id': 'child-thread', 'author_type': 'plugin', 'author_id': 'installation',
                                    'parent_id': None, **json.loads(request.data)})
            return io.BytesIO(json.dumps(self.api.writes[-1]).encode())
        with patch.object(runtime, 'urlopen', side_effect=callback):
            self.assertEqual(RALPH(self.api, self.payload), {'continued': 1})
            self.assertEqual(RALPH(self.api, self.payload), {'continued': 0})
        rule = next(iter(self.api.rules.values()))
        self.assertEqual(rule['issue_id'], 'parent')
        self.assertNotIn('parent_comment_id', rule)
        self.assertEqual(self.api.storage['issue:child'], {'last_run': 'child-run'})

    def test_continues_without_a_project(self):
        self.api.rows['parent']['project_id'] = None
        self.assertEqual(RALPH(self.api, self.payload), {'continued': 1})


class PackagingTests(unittest.TestCase):
    def test_packages_are_single_file_executables_with_declared_triggers(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "in-progress-ralph.zip"
            package(ROOT / "in-progress-ralph", output)
            with ZipFile(output) as bundle:
                self.assertEqual(set(bundle.namelist()), {"multica.plugin.json", "hook.py"})
                code = bundle.read("hook.py")
                self.assertTrue(code.startswith(b"#!/usr/bin/env python3\n"))
                compile(code, "hook.py", "exec")
                manifest = json.loads(bundle.read("multica.plugin.json"))
                for hook in manifest["contributes"]["hooks"]:
                    self.assertEqual(hook["triggers"], ["event"])
                    self.assertEqual(hook["events"], ["task.completed", "task.failed"])
                    self.assertIn("tasks:read", manifest["scopes"])
                    self.assertNotIn("schedule", hook)


if __name__ == "__main__":
    unittest.main()
