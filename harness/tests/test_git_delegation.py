"""Real Git ownership and delivery with path-free issue metadata."""
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

SKILLS = Path(__file__).resolve().parents[1] / 'skills'


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class GitDelegation(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        with patch.object(sys, 'path', [str(SKILLS / 'multica-subissue-distribute'), *sys.path]):
            self.sop = load('native_git_delegation', SKILLS / 'multica-subissue-distribute/controller.py')
        self.base = self.sop.base
        self.rows = {}
        self.sop.issue = self.base.issue = lambda identity, workspace: self.rows[identity]
        self.base.bind = lambda value, key, text: value['metadata'].update({key: text})
        self.repo = self.root / 'repository'
        self.repo.mkdir()
        self.git = lambda *args: self.base.git(self.repo, *args).stdout.strip()
        self.git('init', '-q', '-b', 'main')
        self.git('config', 'user.name', 'Test')
        self.git('config', 'user.email', 'test@localhost')
        (self.repo / 'solution.txt').write_text('baseline\n')
        self.git('add', '.')
        self.git('commit', '-qm', 'Baseline')

    def issue(self, identity, parent=None, tag='dev', **props):
        value = {'id': identity, 'workspace_id': 'workspace', 'parent_issue_id': parent,
                 'metadata': props, 'labels': [{'name': tag}], 'status': 'in_progress',
                 'assignee_type': 'agent', 'assignee_id': 'agent'}
        self.rows[identity] = value
        return value

    def test_supplied_branch_reuses_its_checkout_without_path_metadata(self):
        self.git('checkout', '-qb', 'refinement')
        self.git('remote', 'add', 'origin', 'git@example.com:owner/repo.git')
        root = self.issue('root', git_branch='refinement', git_parent_branch='main')
        bound = self.base.bootstrap(self.repo, root, 'workspace')
        self.assertEqual(bound['workdir'], str(self.repo))
        self.assertEqual(root['metadata'], {'git_repository': 'git@example.com:owner/repo.git',
                         'git_branch': 'refinement', 'git_parent_branch': 'main'})
        self.assertTrue(self.base.owned_branch(self.repo, 'refinement', root, 'workspace'))
        other = self.issue('other', git_branch='refinement', git_parent_branch='main')
        with self.assertRaisesRegex(self.base.WorkflowError, 'not owned'):
            self.base.bootstrap(self.repo, other, 'workspace')

    def test_query_shares_parent_branch_and_dev_gets_a_distinct_branch(self):
        plain = self.issue('plain', tag='query')
        plain['project_id'] = 'research-project'
        self.assertEqual(self.sop.resolve_repository(None, plain, 'workspace'), (None, None))
        parent = self.issue('parent')
        self.base.bootstrap(self.repo, parent, 'workspace')
        query = self.issue('query', parent='parent', tag='query')
        inherited = self.sop.prepare(self.repo, query, 'workspace')
        self.assertEqual(inherited['branch'], parent['metadata']['git_branch'])
        self.assertEqual(self.sop.branch_owner(query, 'workspace')['id'], 'parent')
        self.assertTrue(self.sop.owned_branch(self.repo, inherited['branch'], query, 'workspace'))
        child = self.issue('child', parent='query')
        bound = self.sop.prepare(self.repo, child, 'workspace')
        self.assertNotEqual(bound['branch'], inherited['branch'])
        self.assertEqual(child['metadata']['git_parent_branch'], inherited['branch'])
        self.assertEqual(set(child['metadata']), {'git_branch', 'git_parent_branch'})
        self.assertEqual(set(query['metadata']), {'git_branch', 'git_parent_branch'})

    def review(self):
        folder = SKILLS / 'multica-subissue-review'
        with patch.object(sys, 'path', [str(folder), *sys.path]):
            return load('native_review', folder / 'review_core.py')

    def cli(self, *words, **kwargs):
        if '--tail' in words and '--thread' not in words:
            raise ValueError('tail requires thread')
        if words[0] == 'get':
            return self.rows[words[1]]
        if words[0] == 'children':
            return {'unstaged': [c for c in self.rows.values() if c.get('parent_issue_id') == words[1]], 'stages': []}
        if words[:2] == ('comment', 'list'):
            child = self.rows[words[2]]
            branch = child['metadata'].get('git_branch')
            text = 'Measured delivery and evidence.' + ('\nCommit: ' + self.git('rev-parse', branch) if branch else '')
            return [{'id': 'delivery', 'content': text, 'author_type': 'agent', 'author_id': 'agent'}]
        if words[:2] == ('comment', 'add'):
            self.last_comment = Path(words[words.index('--content-file') + 1]).read_text()
            return {'id': 'review-reply'}
        if words[0] == 'status':
            self.rows[words[1]]['status'] = words[2]
            return
        raise AssertionError(words)

    def accept(self, parent, child, head, verify):
        core = self.review()
        args = SimpleNamespace(child=child['id'], repo=self.repo, commit=head, verify=verify, reason=None)
        context = {'cli': self.cli, 'initial': parent, 'workspace_id': 'workspace',
                   'error': lambda code, message: ValueError(message)}
        with patch.object(core, 'branch_sop', return_value=self.sop):
            return core.decide(args, context, 'accept')

    def test_review_merges_the_child_without_result_or_decision_metadata(self):
        parent = self.issue('parent')
        self.base.bootstrap(self.repo, parent, 'workspace')
        child = self.issue('child', parent='parent')
        bound = self.sop.prepare(self.repo, child, 'workspace')
        common = self.base.repository(self.repo)[1]
        workdir = self.base.worktree(self.repo, common, bound, 'workspace')
        (workdir / 'solution.txt').write_text('accepted\n')
        self.base.git(workdir, 'commit', '-qam', 'Improve solution')
        head = self.git('rev-parse', bound['branch'])
        child['status'] = 'in_review'
        before = dict(child['metadata'])
        self.accept(parent, child, head, 'test "$(cat solution.txt)" = accepted')
        parent_dir = self.base.checkout(self.repo, parent['metadata']['git_branch'])
        self.assertEqual((parent_dir / 'solution.txt').read_text(), 'accepted\n')
        self.assertEqual(child['status'], 'done')
        self.assertEqual(child['metadata'], before)
        self.assertIn(head, self.last_comment)
        self.assertTrue(self.git('branch', '--list', bound['branch']))
        self.assertEqual(self.git('show', 'main:solution.txt'), 'baseline')

    def test_supplied_root_is_verified_without_a_remote_child_sop(self):
        parent = self.issue('parent', tag='query')
        self.git('checkout', '-qb', 'candidate')
        child = self.issue('child', parent='parent', git_branch='candidate', git_parent_branch='main')
        child['status'] = 'in_review'
        self.base.bootstrap(self.repo, child, 'workspace')
        before = dict(child['metadata'])
        self.accept(parent, child, self.git('rev-parse', 'HEAD'), 'test -f solution.txt')
        self.assertEqual(child['status'], 'done')
        self.assertEqual(child['metadata'], before)
        self.assertEqual(self.git('branch', '--show-current'), 'candidate')

    def test_acceptance_rejects_a_commit_missing_from_the_delivery_reply(self):
        parent = self.issue('parent', tag='query')
        self.git('checkout', '-qb', 'candidate')
        child = self.issue('child', parent='parent', git_branch='candidate', git_parent_branch='main')
        child['status'] = 'in_review'
        self.base.bootstrap(self.repo, child, 'workspace')
        core = self.review()
        args = SimpleNamespace(child='child', repo=self.repo, commit=self.git('rev-parse', 'HEAD'), verify='true')
        context = {'cli': self.cli, 'initial': parent, 'workspace_id': 'workspace',
                   'error': lambda code, message: ValueError(message)}
        stale = {'id': 'delivery', 'author_type': 'agent', 'author_id': 'agent', 'content': 'Commit: ' + '0' * 40}
        with patch.object(core, 'replies', return_value=[stale]), patch.object(core, 'branch_sop', return_value=self.sop):
            with self.assertRaisesRegex(ValueError, 'match the child delivery reply'):
                core.decide(args, context, 'accept')
        self.assertEqual(child['status'], 'in_review')

    def test_failed_verification_does_not_complete_the_child(self):
        parent = self.issue('parent', tag='query')
        self.git('checkout', '-qb', 'candidate')
        child = self.issue('child', parent='parent', git_branch='candidate', git_parent_branch='main')
        child['status'] = 'in_review'
        self.base.bootstrap(self.repo, child, 'workspace')
        head = self.git('rev-parse', 'HEAD')
        with self.assertRaises(self.base.WorkflowError):
            self.accept(parent, child, head, 'false')
        self.assertEqual(child['status'], 'in_review')
        self.assertEqual(self.git('rev-parse', 'candidate'), head)

    def test_group_review_uses_reply_evidence_and_settles_in_the_parent_turn(self):
        parent = self.issue('parent')
        self.base.bootstrap(self.repo, parent, 'workspace')
        for identity in ('first', 'second'):
            child = self.issue(identity, parent='parent', tag='exp', experiment_group='trial',
                               experiment_group_size='2', experiment_baseline='1.0', experiment_direction='min')
            binding = self.sop.prepare(self.repo, child, 'workspace')
            directory = self.base.worktree(self.repo, self.base.repository(self.repo)[1], binding, 'workspace')
            (directory / (identity + '.txt')).write_text(identity)
            self.base.git(directory, 'add', '.')
            self.base.git(directory, 'commit', '-qm', identity)
            child['status'] = 'in_review'
        before = {key: dict(row['metadata']) for key, row in self.rows.items()}
        folder = SKILLS / 'multica-subissue-review'
        with patch.object(sys, 'path', [str(folder), *sys.path]):
            controller = load('native_group_review', folder / 'controller.py')
        def cli(*words, **kwargs):
            if words[:2] == ('comment', 'list'):
                reply = self.cli(*words, **kwargs)[0]
                reply['content'] = 'Score: ' + ('0.42' if words[2] == 'first' else '0.55') + '\n' + reply['content']
                return [reply]
            return self.cli(*words, **kwargs)
        head = self.git('rev-parse', self.rows['first']['metadata']['git_branch'])
        args = SimpleNamespace(repo=self.repo, child='first', winner='first', commit=head, verify='test -f first.txt')
        context = {'args': args, 'cli': cli, 'initial': parent, 'issue_id': parent['id'],
                   'workspace_id': 'workspace', 'phase': 'comparison',
                   'error': lambda code, message: ValueError(message)}
        with patch.object(controller.core, 'branch_sop', return_value=self.sop):
            view = controller.comparison_view(context)
            self.assertIn('Score: 0.42', json.dumps(view))
            self.assertIn('Score: 0.55', json.dumps(view))
            controller.select(args, context)
            controller.done(args, context)
        self.assertEqual(self.rows['first']['status'], 'done')
        self.assertEqual(self.rows['second']['status'], 'cancelled')
        self.assertEqual(parent['status'], 'in_progress')
        self.assertEqual({key: row['metadata'] for key, row in self.rows.items()}, before)

    def test_project_resource_resolves_checkout_without_issue_paths(self):
        root = self.issue('root', git_repository='git@example.com:owner/repo.git')
        root['project_id'] = 'project'
        self.git('remote', 'add', 'origin', root['metadata']['git_repository'])
        payload = {'resources': [{'resource_type': 'local_directory', 'resource_ref': {
            'local_path': str(self.repo), 'daemon_id': 'daemon'}}]}
        native_command = self.base.command
        def command(argv, **kwargs):
            if argv[:4] == ['multica', 'project', 'resource', 'list']:
                return SimpleNamespace(stdout=json.dumps(payload))
            return native_command(argv, **kwargs)
        with patch.object(self.base, 'command', side_effect=command), patch.dict(os.environ, MULTICA_DAEMON_ID='daemon'):
            resolved, _ = self.base.resolve_repository(None, root, 'workspace')
        self.assertEqual(resolved, self.repo)
        self.assertEqual(root['metadata'], {'git_repository': 'git@example.com:owner/repo.git'})


if __name__ == '__main__':
    unittest.main()
