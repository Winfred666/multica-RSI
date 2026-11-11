"""One inbox wait returns the oldest unread event through the default read API."""
import importlib.util
import io
import json
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('workspace_inbox', Path(__file__).resolve().parents[1] /
                                            'skills/multica-workspace-manipulation/inbox.py')
inbox = importlib.util.module_from_spec(spec)
spec.loader.exec_module(inbox)


class InboxWait(unittest.TestCase):
    def test_blocks_then_reads_one_oldest_notification(self):
        oldest = {'id': 'ffffffff-ffff-4fff-afff-ffffffffffff', 'read': False, 'archived': False}
        newest = {**oldest, 'id': '00000000-0000-4000-a000-000000000000'}
        full = {**oldest, 'read': True, 'workspace_id': 'target', 'issue_id': 'issue',
                'title': 'Trial', 'type': 'new_comment', 'actor_type': 'agent',
                'actor_id': 'worker', 'body': 'Full comment ' * 50,
                'details': {'comment_id': 'comment'}, 'issue_status': 'in_review'}
        responses = [[], [newest, {**oldest, 'read': True}, oldest,
                              {**oldest, 'archived': True}], full]
        requests = []

        def open_request(req, timeout):
            requests.append(req)
            self.assertEqual(timeout, 30)
            self.assertEqual(req.get_header('Authorization'), 'Bearer member-token')
            self.assertEqual(req.get_header('X-workspace-id'), 'target')
            return io.StringIO(json.dumps(responses.pop(0)))

        with patch.object(inbox, 'urlopen', open_request), patch.object(inbox.time, 'sleep') as sleep:
            result = inbox.wait('https://example.test/', 'member-token', 'target')
        sleep.assert_called_once_with(5)
        self.assertEqual([(r.method, r.full_url) for r in requests], [
            ('GET', 'https://example.test/api/inbox'), ('GET', 'https://example.test/api/inbox'),
            ('POST', 'https://example.test/api/inbox/' + oldest['id'] + '/read')])
        self.assertEqual(result['body'], full['body'])
        self.assertEqual(result['actor_id'], 'worker')
        self.assertIn('unless target work is complete or acceptable', result['next'])
