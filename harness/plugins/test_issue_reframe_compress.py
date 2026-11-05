import copy
import json
from pathlib import Path
import tempfile
import unittest
from zipfile import ZipFile

from test_ralph import FakeAPI, ROOT, package, policy


PRUNE = policy('issue-reframe-compress')


class PruningAPI(FakeAPI):
    def __init__(self):
        super().__init__()
        self.comments = []
        self.deleted = []
        self.rows['parent']['project_id'] = None

    def request(self, method, path, body=None, **kwargs):
        if path == '/api/issues/parent/comments?fold=false':
            if self.truncated:
                kwargs['response_headers']['X-Comments-Truncated'] = 'true'
            return copy.deepcopy(self.comments)
        if path.startswith('/api/comments/') and method == 'DELETE':
            comment_id = path.split('/')[3]
            self.deleted.append(comment_id)
            if any(row.get('parent_id') == comment_id for row in self.comments):
                next(row for row in self.comments if row['id'] == comment_id)['deleted_at'] = 'now'
            else:
                self.comments = [row for row in self.comments if row['id'] != comment_id]
            return None
        return super().request(method, path, body, **kwargs)


class PruningTests(unittest.TestCase):
    def setUp(self):
        self.api = PruningAPI()
        self.event = {'trigger': 'event', 'event_type': 'task.completed', 'issue_id': 'parent',
                      'input': {'task_id': 'run'}}
        self.review = {'trigger': 'agent', 'hook_key': 'comments', 'actor': {'type': 'agent', 'id': 'agent'},
                       'input': {'issue_id': 'parent'}}
        self.api.tasks['parent'] = [{'id': 'run', 'agent_id': 'agent', 'status': 'completed',
                                    'input_trend': {'context_compression_count': 2}}]

    def row(self, name, parent=None, resolved=False):
        row = {'id': name, 'parent_id': parent, 'content': name, 'revision': 1,
               'created_at': '2025-01-01T00:00:00Z', 'updated_at': '2025-01-01T00:00:00Z',
               'thread_resolved': resolved}
        self.api.comments.append(row)
        return row

    def decide(self, result, action):
        data = copy.deepcopy(self.review)
        data['input']['decisions'] = [{'id': row['id'], 'revision': row['revision'], 'action': action}
                                     for row in result['items']]
        return PRUNE(self.api, data)

    def test_completed_events_schedule_once_at_even_thresholds_for_any_issue(self):
        self.api.status('parent', 'done')
        for count, expected in [(1, False), (2, True), (2, False), (3, False), (4, True), (6, True)]:
            self.api.tasks['parent'][0]['input_trend']['context_compression_count'] = count
            self.assertEqual(PRUNE(self.api, self.event)['scheduled'], expected)
        self.assertEqual(len(self.api.rules), 3)
        self.assertEqual(self.api.storage['issue:parent:agent']['threshold'], 6)

    def test_five_comments_then_five_closed_threads_showing_only_final_comment(self):
        for i in range(6):
            self.row(f'open-{i}')
            self.row(f'closed-{i}', resolved=True)
            self.row(f'middle-{i}', parent=f'closed-{i}')
            last = self.row(f'last-{i}', parent=f'middle-{i}')
            last['created_at'] = '2025-01-03T00:00:00Z'
        PRUNE(self.api, self.event)
        result = PRUNE(self.api, self.review)
        self.assertEqual((result['mode'], len(result['items'])), ('comments', 5))
        result = self.decide(result, 'keep')
        self.assertEqual((result['mode'], len(result['items'])), ('comments', 1))
        result = self.decide(result, 'keep')
        self.assertEqual((result['mode'], len(result['items'])), ('threads', 5))
        self.assertEqual([row['content'] for row in result['items']], [f'last-{i}' for i in range(5)])
        self.assertNotIn('middle-', json.dumps(result))
        result = self.decide(result, 'delete')
        self.assertEqual(self.api.deleted[:3], ['last-0', 'middle-0', 'closed-0'])
        self.assertEqual(len(self.api.deleted), 15)
        self.assertTrue(self.decide(result, 'keep')['complete'])
        self.assertEqual(self.api.storage['issue:parent:agent'], {'threshold': 2})

    def test_protects_active_inputs_and_refuses_changed_items_or_rewrites(self):
        first = self.row('old')
        self.row('active')
        self.row('closed', resolved=True)
        self.row('active-reply', parent='closed')
        self.api.tasks['parent'].append({'id': 'busy', 'status': 'running', 'agent_id': 'agent',
            'trigger_comment_id': 'active', 'delivered_comment_ids': ['active-reply']})
        PRUNE(self.api, self.event)
        result = PRUNE(self.api, self.review)
        self.assertEqual([row['id'] for row in result['items']], ['old'])
        with self.assertRaisesRegex(ValueError, 'invalid'):
            self.decide(result, 'update')
        first['revision'] = 2
        with self.assertRaisesRegex(ValueError, 'changed'):
            self.decide(result, 'delete')
        self.assertEqual(self.api.deleted, [])
        self.api.truncated = True
        with self.assertRaisesRegex(ValueError, 'truncated'):
            PRUNE(self.api, self.review)

    def test_packaged_pruning_hook_is_executable(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / 'pruning.zip'
            package(ROOT / 'issue-reframe-compress', archive)
            with ZipFile(archive) as bundle:
                self.assertEqual(set(bundle.namelist()), {'multica.plugin.json', 'hook.py'})
                compile(bundle.read('hook.py'), 'hook.py', 'exec')
                manifest = json.loads(bundle.read('multica.plugin.json'))
                self.assertEqual(manifest['name'], 'Comment and thread pruning')
                self.assertEqual(manifest['key'], 'org.multica.issue-reframe-compress')
                self.assertEqual(manifest['contributes']['hooks'][0]['events'], ['task.completed'])


if __name__ == '__main__':
    unittest.main()
