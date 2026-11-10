"""Blocking requests parent help without a duplicate child note."""
import importlib.util
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
        self.issue = {'id': 'child', 'workspace_id': 'workspace', 'parent_issue_id': 'parent', 'status': 'in_progress'}
        self.writes = []

    def cli(self, *words, **kwargs):
        if words[0] == 'get':
            return self.issue
        if words[0] == 'status':
            self.writes.append(('status', words[2]))
            self.issue['status'] = words[2]
            return
        raise AssertionError(words)

    def test_blocker_finishes_only_after_parent_help_dispatch(self):
        block = load('native_block', 'multica-surface-block', 'controller.py')
        parent = {'id': 'parent', 'workspace_id': 'workspace', 'status': 'in_progress',
                  'assignee_type': 'agent', 'assignee_id': 'parent-agent'}
        args = SimpleNamespace(reason='Please approve the required workspace access.', parent_thread='parent-reply')
        for parent_thread, dispatch in (('parent-reply', 'queued'), (None, 'queued'),
                                        ('parent-reply', 'steered'), ('parent-reply', 'coalesced'),
                                        ('parent-reply', 'deferred'), ('parent-reply', 'blocked'),
                                        ('parent-reply', None)):
            args.parent_thread = parent_thread
            with self.subTest(parent_thread=parent_thread, dispatch=dispatch), tempfile.TemporaryDirectory() as directory:
                self.issue['status'] = 'in_progress'
                writes = []

                def cli(*words, **kwargs):
                    if words == ('get', 'parent'):
                        return parent
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

                context = {'initial': self.issue, 'cli': cli, 'issue_id': 'child', 'state': {}, 'save_state': lambda: None,
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
                        self.assertIsNone(block.block(args, context))
                        block.block(args, context)
                self.assertEqual([row[0] for row in writes], ['status', 'parent'])
                request = writes[-1]
                self.assertIn('mention://agent/parent-agent', request[1])
                self.assertIn(args.reason, request[1])
                self.assertIn('mention://issue/child', request[1])
                self.assertNotIn('/note', request[1])
                self.assertIn('--steer-active', request[2])
                if parent_thread:
                    self.assertEqual(request[2][-3:-1], ('--parent', parent_thread))
                else:
                    self.assertNotIn('--parent', request[2])

    def test_root_blocker_leaves_explanation_to_normal_reply(self):
        block = load('native_block_root', 'multica-surface-block', 'controller.py')
        self.issue['parent_issue_id'] = None
        context = {'initial': self.issue, 'cli': self.cli, 'issue_id': 'child', 'state': {},
                   'save_state': lambda: None, 'error': lambda code, message: ValueError(message)}
        self.assertIsNone(block.block(SimpleNamespace(reason='Need owner approval.', parent_thread=None), context))
        self.assertEqual(self.writes, [('status', 'blocked')])


if __name__ == '__main__':
    unittest.main()
