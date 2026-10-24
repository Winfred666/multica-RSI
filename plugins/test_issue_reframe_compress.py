import copy
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest
from zipfile import ZipFile

from package import package
from test_workflows import FakeAPI, policy


COMPRESS = policy("issue-reframe-compress")


class CommentAPI(FakeAPI):
    def __init__(self):
        super().__init__()
        self.comments = []
        self.comment_writes = []
        self.truncated = False

    def request(self, method, path, body=None, missing=None, response_headers=None):
        if path.endswith("/comments?fold=true"):
            if response_headers is not None and self.truncated:
                response_headers["X-Comments-Truncated"] = "true"
            return copy.deepcopy(self.comments)
        if path.startswith("/api/comments/"):
            comment_id = path.split("/")[3]
            row = next(row for row in self.comments if row["id"] == comment_id)
            self.comment_writes.append((method, comment_id, body))
            if method == "DELETE":
                if any(child.get("parent_id") == comment_id for child in self.comments):
                    row.update(content="", deleted_at="2026-01-01T00:02:00Z", attachments=[])
                else:
                    self.comments.remove(row)
            else:
                assert body["expected_revision"] == row["revision"]
                row.update(content=body["content"], attachments=[], revision=row["revision"] + 1,
                           updated_at="2026-01-01T00:02:00Z")
            return copy.deepcopy(row)
        return super().request(method, path, body, missing)


class IssueCompressTests(unittest.TestCase):
    def setUp(self):
        self.api = CommentAPI()
        self.api.rows.pop("child")
        self.payload = {"hook_key": "comments", "trigger": "agent",
                        "actor": {"type": "agent", "id": "agent"}, "input": {"issue_id": "parent"}}
        self.set_count(0)

    def set_count(self, count, agent="agent"):
        self.api.tasks["parent"] = [{
            "id": "run", "agent_id": agent, "status": "completed",
            "created_at": "2026-01-01T00:00:00Z", "completed_at": "2026-01-01T00:01:00Z",
            "input_trend": {"context_compression_count": count},
        }, {"id": "active", "agent_id": "agent", "status": "running",
            "created_at": "2026-01-01T00:01:00Z", "started_at": "2026-01-01T00:01:00Z"}]

    def check(self):
        return COMPRESS(self.api, self.payload)

    def complete(self, agent="agent", decisions=None):
        return COMPRESS(self.api, {"hook_key": "comments", "trigger": "agent",
                                  "actor": {"type": "agent", "id": agent},
                                  "input": {"issue_id": "parent", "decisions": decisions or []}})

    def test_two_compressions_clean_within_existing_run_then_reset(self):
        self.comment("root")
        self.comment("z-last", "root")
        for count in (0, 1):
            self.set_count(count)
            self.assertTrue(self.check()["complete"])
        self.set_count(2)
        self.assertFalse(self.check()["complete"])
        self.assertEqual(self.api.writes, [])
        self.assertTrue(self.complete(decisions=[{"id": "root", "revision": 1, "action": "preserve"}])["complete"])
        self.assertEqual(self.api.storage["issue:parent:agent"], {"baseline": 2})
        self.set_count(3)
        self.assertTrue(self.check()["complete"])
        self.set_count(4)
        self.assertFalse(self.check()["complete"])
        self.assertEqual(self.api.writes, [])

    def test_never_starts_a_task_or_handles_automatic_triggers(self):
        for trigger in ("event", "schedule", "manual"):
            with self.assertRaises(ValueError):
                COMPRESS(self.api, dict(self.payload, trigger=trigger))
        self.api.tasks["parent"].pop()
        with self.assertRaises(ValueError):
            self.check()
        self.assertEqual(self.api.writes, [])

    def test_missing_counter_and_other_agent_do_not_trigger(self):
        self.set_count(99, agent="other")
        self.assertTrue(self.check()["complete"])
        self.set_count(99)
        self.api.tasks["parent"][0].pop("input_trend")
        self.assertTrue(self.check()["complete"])
        self.assertEqual(self.api.storage, {})

    def test_only_current_assignee_can_review(self):
        self.set_count(2)
        with self.assertRaises(PermissionError):
            self.complete(agent="other")
        self.api.rows["parent"]["assignee_id"] = "other"
        with self.assertRaises(PermissionError):
            self.complete()
        self.assertEqual(self.api.storage, {})

    def test_returns_latest_delivered_steer_after_cleanup(self):
        self.comment("root")
        self.comment("trigger", "root", content="Run the original experiment.")
        self.comment("z-last", "root")
        self.set_count(2)
        task = self.api.tasks["parent"][-1]
        task["trigger_comment_id"] = "trigger"
        self.check()
        self.comment("steer", "root", content="Use the corrected dataset first.",
                     created_at="2026-01-01T00:02:00Z", updated_at="2026-01-01T00:02:00Z")
        task["delivered_comment_ids"] = ["trigger", "steer"]
        result = self.complete(decisions=[{"id": "root", "revision": 1, "action": "preserve"},
                                          {"id": "z-last", "revision": 1, "action": "preserve"}])
        self.assertTrue(result["complete"])
        self.assertEqual(result["instruction"], "Continue the latest instruction:\nUse the corrected dataset first.")
        self.assertEqual(self.api.writes, [])

    def test_returns_handoff_or_issue_continuation(self):
        self.set_count(2)
        self.api.tasks["parent"][-1]["handoff_note"] = "Finish the evaluation."
        self.assertEqual(self.check()["instruction"], "Continue the instruction that started this run:\nFinish the evaluation.")
        self.api.tasks["parent"][-1].pop("handoff_note")
        self.assertEqual(self.check()["instruction"], "Continue solving the issue.")

    def comment(self, comment_id, parent=None, **values):
        row = {"id": comment_id, "issue_id": "parent", "parent_id": parent,
               "author_type": "agent", "content": "An important experiment", "revision": 1,
               "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
               "attachments": [{"id": "artifact"}]}
        row.update(values)
        self.api.comments.append(row)
        return row

    def pending(self):
        self.set_count(2)
        self.check()

    def test_system_comments_deleted_and_resolved_threads_never_offered(self):
        self.comment("resolved", thread_resolved=True)
        self.comment("solution", "resolved", resolved_at="2026-01-01T00:00:01Z")
        self.comment("system", author_type="system", content="system steering")
        self.comment("root")
        self.comment("z-last", "root")
        self.pending()
        result = self.complete()
        self.assertEqual([row["id"] for row in result["comments"]], ["root"])
        self.assertEqual(self.api.comment_writes, [("DELETE", "system", None)])
        self.assertFalse(result["complete"])
        self.assertEqual(self.api.storage["issue:parent:agent"]["baseline"], 0)

    def test_assignee_delete_update_preserve_and_automatic_completion(self):
        self.comment("root")
        self.comment("a-noise", "root")
        self.comment("b-result", "root")
        important = self.comment("c-experiment", "root")
        latest = self.comment("z-last", "root")
        self.pending()
        result = self.complete(decisions=[
            {"id": "root", "revision": 1, "action": "preserve"},
            {"id": "a-noise", "revision": 1, "action": "delete"},
            {"id": "b-result", "revision": 1, "action": "update", "content": "The experiment reduced loss."},
            {"id": "c-experiment", "revision": 1, "action": "preserve"},
        ])
        self.assertTrue(result["complete"])
        self.assertEqual(self.api.storage["issue:parent:agent"], {"baseline": 2})
        self.assertEqual([write[:2] for write in self.api.comment_writes], [("DELETE", "a-noise"), ("PUT", "b-result")])
        self.assertEqual(important["attachments"], [{"id": "artifact"}])
        self.assertEqual(latest["attachments"], [{"id": "artifact"}])
        updated = next(row for row in self.api.comments if row["id"] == "b-result")
        self.assertEqual(updated["content"], "The experiment reduced loss.")
        self.assertEqual(updated["attachments"], [])

    def test_protects_run_inputs_latest_reply_new_edits_and_pending_supplements(self):
        self.comment("root")
        for comment_id in ("trigger", "coalesced", "delivered", "supplement"):
            self.comment(comment_id, "root", author_type="system")
        self.comment("new-edit", "root", updated_at="2026-01-01T00:02:00Z")
        self.comment("in-flight", "root", supplements=[{"status": "delivering"}])
        self.comment("z-last", "root")
        self.set_count(2)
        self.api.tasks["parent"][-1].update({
            "trigger_comment_id": "trigger",
            "coalesced_comment_ids": ["coalesced"], "delivered_comment_ids": ["delivered"],
            "supplement_comment_ids": ["supplement"],
        })
        result = self.complete()
        self.assertEqual([row["id"] for row in result["comments"]], ["root"])
        with self.assertRaises(ValueError):
            self.complete(decisions=[{"id": "z-last", "revision": 1, "action": "delete"}])
        self.assertEqual(self.api.comment_writes, [])

    def test_stale_revision_and_truncated_input_do_not_mutate(self):
        self.comment("root", revision=2)
        self.comment("z-last", "root")
        self.pending()
        with self.assertRaises(ValueError):
            self.complete(decisions=[{"id": "root", "revision": 1, "action": "delete"}])
        self.api.truncated = True
        with self.assertRaisesRegex(ValueError, "truncated"):
            self.complete()
        self.assertEqual(self.api.comment_writes, [])

    def test_update_rejects_links_and_requires_short_plain_text(self):
        self.comment("root")
        self.comment("z-last", "root")
        self.pending()
        for content in ("", "read https://example.com", "two\nlines", "x" * 501):
            with self.assertRaises(ValueError):
                self.complete(decisions=[{"id": "root", "revision": 1, "action": "update", "content": content}])
        self.assertEqual(self.api.comment_writes, [])

    def test_package_is_self_contained(self):
        source = Path(__file__).parent / "issue-reframe-compress"
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "plugin.zip"
            package(source, output)
            with ZipFile(output) as bundle:
                compile(bundle.read("hook.py"), "hook.py", "exec")
                manifest = json.loads(bundle.read("multica.plugin.json"))
                self.assertEqual(manifest["key"], "com.multica.issue-reframe-compress")
                self.assertEqual(manifest["contributes"]["hooks"][0]["triggers"], ["agent"])

    def test_packaged_hook_over_http_lists_and_applies_decisions(self):
        self.set_count(2)
        self.comment("root")
        self.comment("system", "root", author_type="system")
        self.comment("z-last", "root")
        api = self.api

        class Handler(BaseHTTPRequestHandler):
            def handle_request(self):
                body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
                data = api.request(self.command, self.path, json.loads(body) if body else None)
                self.send_response(200)
                self.end_headers()
                self.wfile.write(json.dumps(data).encode())

            do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = handle_request

            def log_message(self, *_):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as directory:
                output = Path(directory) / "plugin.zip"
                package(Path(__file__).parent / "issue-reframe-compress", output)
                with ZipFile(output) as bundle:
                    script = Path(directory) / "hook.py"
                    script.write_bytes(bundle.read("hook.py"))
                env = dict(os.environ, MULTICA_SERVER_URL=f"http://127.0.0.1:{server.server_port}",
                           MULTICA_WORKSPACE_ID="workspace", MULTICA_TOKEN="test")

                def invoke(payload):
                    payload.update(workspace_id="workspace", installation_id="installation")
                    result = subprocess.run([sys.executable, str(script)], input=json.dumps(payload),
                                            text=True, capture_output=True, env=env, timeout=10)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    return json.loads(result.stdout)

                payload = {"hook_key": "comments", "trigger": "agent", "actor": {"type": "agent", "id": "agent"},
                           "input": {"issue_id": "parent"}}
                self.assertEqual([row["id"] for row in invoke(payload)["comments"]], ["root"])
                payload["input"]["decisions"] = [{"id": "root", "revision": 1, "action": "update", "content": "The experiment improved accuracy."}]
                self.assertTrue(invoke(payload)["complete"])
                self.assertEqual(api.storage["issue:parent:agent"], {"baseline": 2})
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


if __name__ == "__main__":
    unittest.main()
