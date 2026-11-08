"""Child delivery and blocking publish native replies before changing status."""
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

SKILLS = Path(__file__).resolve().parents[1] / 'skills'


def load(name, folder, file):
    with patch.object(sys, 'path', [str(SKILLS / folder), *sys.path]):
        spec = importlib.util.spec_from_file_location(name, SKILLS / folder / file)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module


class CommentHandoff(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)
        self.issue = {'id': 'child', 'workspace_id': 'workspace', 'parent_issue_id': 'parent', 'status': 'in_progress'}
        self.comments = [{'id': 'thread', 'issue_id': 'child', 'parent_id': None, 'content': 'Task'}]
        self.writes = []

    def cli(self, *words, **kwargs):
        if words[0] == 'get':
            return self.issue
        if words[:2] == ('comment', 'list'):
            return self.comments
        if words[:2] == ('comment', 'add'):
            content = Path(words[words.index('--content-file') + 1]).read_text()
            comment = {'id': 'reply', 'issue_id': 'child', 'parent_id': 'thread', 'content': content}
            self.comments.append(comment)
            self.writes.append(('reply', content))
            return comment
        if words[:2] == ('comment', 'resolve'):
            self.comments[0]['resolved_at'] = 'now'
            return
        if words[0] == 'status':
            self.writes.append(('status', words[2]))
            self.issue['status'] = words[2]
            return
        raise AssertionError(words)

    def test_delivery_report_and_git_commit_live_only_in_the_reply(self):
        deliver = load('native_deliver', 'multica-surface-delivery', 'deliver.py')
        report = self.directory / 'report.json'
        report.write_text(json.dumps({'claim': 'Improved result', 'metric': 0.42, 'method': 'Measured on the benchmark.', 'appendix': 'Run test.py.'}))
        facts = {'tag': 'exp', 'threads': {'thread': 'thread'}, 'workdir': str(self.directory),
                 'branch': 'issue/child', 'attachments': [], 'allow_external_file': False,
                 'issue_id': 'child', 'parent_id': 'parent', 'target_status': 'in_review'}
        receipt = {'id': 'task', 'head': 'abc123', 'facts': facts}
        context = {'cli': self.cli, 'error': lambda code, message, hint=None: ValueError(message)}
        args = SimpleNamespace(report_file=report, retry_post=False)
        result = deliver.publish(args, context, self.issue, self.directory / 'checkpoint.json', receipt)
        self.assertEqual(result, {'comment_ids': ['reply']})
        self.assertIn('0.42', self.comments[-1]['content'])
        self.assertIn('Git branch: issue/child', self.comments[-1]['content'])
        self.assertIn('Commit: abc123', self.comments[-1]['content'])
        self.assertEqual([kind for kind, _ in self.writes], ['reply', 'status'])
        self.assertEqual(self.issue['status'], 'in_review')
        self.assertNotIn('metadata', self.issue)

    def test_r206_blocker_finishes_only_after_parent_help_dispatch(self):
        block = load('native_block', 'multica-surface-block', 'controller.py')
        parent = {'id': 'parent', 'workspace_id': 'workspace', 'status': 'in_progress',
                  'assignee_type': 'agent', 'assignee_id': 'parent-agent'}
        args = SimpleNamespace(reason='Frontend must hide /note while preserving routing.', parent_thread='parent-thread')
        for dispatch in ('queued', 'steered', 'coalesced', 'deferred', 'blocked', None):
            with self.subTest(dispatch=dispatch), tempfile.TemporaryDirectory() as directory:
                self.issue['status'] = 'in_progress'
                writes = []

                def cli(*words, **kwargs):
                    if words == ('get', 'parent'):
                        return parent
                    if words[:3] == ('comment', 'list', 'parent'):
                        return [{'id': 'parent-thread', 'issue_id': 'parent'}]
                    if words[:2] == ('comment', 'add'):
                        content = Path(words[words.index('--content-file') + 1]).read_text()
                        writes.append((words[2], content, words))
                        return {'id': words[2] + '-reply', 'trigger_outcomes': [] if dispatch is None else [
                            {'target_type': 'agent', 'target_id': 'parent-agent', 'status': dispatch}]}
                    if words[0] == 'status':
                        self.issue['status'] = words[2]
                        writes.append(('status', words[2], words))
                        return
                    return self.cli(*words, **kwargs)

                context = {'initial': self.issue, 'cli': cli, 'issue_id': 'child', 'task_id': 'r206-regression',
                           'error': lambda code, message: ValueError(message)}
                with patch.object(block.Path, 'cwd', return_value=Path(directory)):
                    if dispatch in ('blocked', None):
                        with self.assertRaisesRegex(ValueError, 'dispatch was not confirmed'):
                            block.block(args, context)
                        # A blocked child's init must resume, not claim a completed handoff.
                        self.assertEqual(block.precheck(context)['status'], 'blocked')
                        with self.assertRaises(ValueError):
                            block.block(args, context)
                    else:
                        result = block.block(args, context)
                        self.assertEqual(result['data']['parent_comment_id'], 'parent-reply')
                        self.assertEqual(result['data']['parent_dispatch'], dispatch)
                        block.block(args, context)
                self.assertEqual([row[0] for row in writes], ['child', 'status', 'parent'])
                request = writes[-1]
                self.assertIn('mention://agent/parent-agent', request[1])
                self.assertIn(args.reason, request[1])
                self.assertIn('mention://issue/child', request[1])
                self.assertIn('child-reply', request[1])
                self.assertIn('--steer-active', request[2])
                self.assertEqual(request[2][-3:-1], ('--parent', 'parent-thread'))


if __name__ == '__main__':
    unittest.main()
