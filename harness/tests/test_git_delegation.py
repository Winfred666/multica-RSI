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
        self.comments = {}
        self.calls = []
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
        self.calls.append(words)
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
            rows = [{'id': 'delivery', 'content': text, 'author_type': 'agent', 'author_id': 'agent', 'created_at': '0'}]
            rows += self.comments.get(child['id'], [])
            if '--roots-only' in words:
                self.assertNotIn('--recent', words)
                return [{'id': 'delivery', 'content': 'Folded thread preview', 'reply_count': len(rows)}]
            self.assertIn('--tail', words)
            return rows
        if words[:2] == ('comment', 'add'):
            self.last_comment = Path(words[words.index('--content-file') + 1]).read_text()
            row = {'id': 'review-reply', 'content': self.last_comment, 'author_type': 'agent',
                   'author_id': 'agent', 'created_at': '1',
                   'parent_id': words[words.index('--parent') + 1] if '--parent' in words else None}
            self.comments.setdefault(words[2], []).append(row)
            return row
        if words[0] == 'status':
            self.rows[words[1]]['status'] = words[2]
            return
        raise AssertionError(words)

    def accept(self, parent, child, head, verify=None, action='accept'):
        core = self.review()
        args = SimpleNamespace(child=child['id'], repo=self.repo, commit=head, verify=verify, reason=None)
        context = {'cli': self.cli, 'initial': parent, 'workspace_id': 'workspace',
                   'error': lambda code, message: ValueError(message)}
        with patch.object(core, 'branch_sop', return_value=self.sop):
            return core.decide(args, context, action)

    def remote(self):
        remote = self.root / 'origin.git'
        self.git('init', '--bare', '-q', str(remote))
        self.git('remote', 'add', 'origin', str(remote))

    def integration_reply(self, child, commit, integrated):
        self.comments[child['id']].append({'id': 'integration', 'created_at': '2', 'author_type': 'agent',
            'author_id': 'agent', 'parent_id': 'review-reply', 'content': 'Verification passed and pushed.\nCommit: ' + commit +
            '\nIntegrated commit: ' + commit + '\nIntegrated head: ' + integrated})
        child['status'] = 'in_review'

    def test_correction_steers_the_existing_delivery_thread(self):
        parent = self.issue('parent')
        child = self.issue('child', parent='parent')
        child['status'] = 'blocked'
        core = self.review()
        args = SimpleNamespace(child=child['id'], reason='Correct the reported issue.')
        context = {'cli': self.cli, 'initial': parent, 'error': lambda code, message: ValueError(message)}
        core.decide(args, context, 'changes')
        self.assertEqual(self.comments[child['id']][-1]['parent_id'], 'delivery')
        self.assertTrue(any('--steer-active' in call for call in self.calls))
        self.assertEqual(child['status'], 'in_progress')

    def test_review_steers_child_then_settles_and_requests_same_direction_siblings(self):
        parent = self.issue('parent', git_branch='main', git_parent_branch='main')
        self.base.bootstrap(self.repo, parent, 'workspace')
        child = self.issue('child', parent='parent')
        child['labels'].append({'name': 'Harness'})
        bound = self.sop.prepare(self.repo, child, 'workspace')
        workdir = self.base.worktree(self.repo, self.base.repository(self.repo)[1], bound, 'workspace')
        (workdir / 'solution.txt').write_text('accepted\n')
        self.base.git(workdir, 'commit', '-qam', 'Improve solution')
        head = self.git('rev-parse', bound['branch'])
        child['status'] = 'in_review'
        before = dict(child['metadata'])
        baseline = self.git('rev-parse', 'main')
        self.remote()
        self.accept(parent, child, head, 'test "$(cat solution.txt)" = accepted')
        self.assertEqual(self.git('rev-parse', 'main'), baseline)
        self.assertEqual(child['status'], 'in_progress')
        self.assertIn('Reviewed commit: ' + head, self.last_comment)
        self.assertIn('push `origin main`', self.last_comment)
        self.assertTrue(any('--steer-active' in call for call in self.calls))
        self.assertEqual(self.comments[child['id']][-1]['parent_id'], 'delivery')
        self.assertEqual(child['metadata'], before)
        with self.assertRaisesRegex(ValueError, 'delivered or blocked'):
            self.accept(parent, child, head, action='settle')
        # The child performs the integration, verification and push in its own turn.
        self.git('merge', '--no-edit', head)
        self.base.command(['/bin/sh', '-c', 'test "$(cat solution.txt)" = accepted'], cwd=self.repo)
        self.integration_reply(child, head, self.git('rev-parse', 'main'))
        with self.assertRaises(self.base.WorkflowError):
            self.accept(parent, child, head, action='settle')
        self.assertEqual(child['status'], 'in_review')
        self.git('push', 'origin', 'main')
        core = self.review()
        context = {'cli': self.cli, 'initial': parent, 'workspace_id': 'workspace', 'args': SimpleNamespace(repo=self.repo)}
        with patch.object(core, 'branch_sop', return_value=self.sop):
            options = core.overview(context)['options']
        self.assertTrue(any(option.name == 'settle' for option in options))
        for identity, direction, status in [('working', 'Harness', 'in_progress'), ('review', 'Harness', 'in_review'),
                                              ('blocked', 'Harness', 'blocked'), ('parked', 'Harness', 'backlog'),
                                              ('finished', 'Harness', 'done'), ('backend', 'Backend', 'in_progress')]:
            sibling = self.issue(identity, parent='parent')
            sibling['labels'].append({'name': direction})
            sibling['status'] = status
        result = self.accept(parent, child, head, action='settle')
        self.assertEqual(child['status'], 'done')
        self.assertEqual(self.comments[child['id']][-1]['parent_id'], 'integration')
        self.assertEqual(result['data']['sibling_rebases'], ['working', 'review', 'blocked'])
        self.assertIn('From this parent issue', result['instruction'])
        self.assertIn('true feature conflicts', result['instruction'])
        self.assertEqual(child['metadata'], before)
        self.assertTrue(self.git('branch', '--list', bound['branch']))

    def test_supplied_root_is_steered_without_merging_in_parent(self):
        parent = self.issue('parent', tag='query')
        self.git('checkout', '-qb', 'candidate')
        child = self.issue('child', parent='parent', git_branch='candidate', git_parent_branch='main')
        child['status'] = 'in_review'
        self.base.bootstrap(self.repo, child, 'workspace')
        before = dict(child['metadata'])
        self.accept(parent, child, self.git('rev-parse', 'HEAD'), 'test -f solution.txt')
        self.assertEqual(child['status'], 'in_progress')
        self.assertIn('Integration target: candidate', self.last_comment)
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

    def test_missing_integration_report_does_not_complete_the_child(self):
        parent = self.issue('parent', tag='query')
        self.git('checkout', '-qb', 'candidate')
        child = self.issue('child', parent='parent', git_branch='candidate', git_parent_branch='main')
        child['status'] = 'in_review'
        self.base.bootstrap(self.repo, child, 'workspace')
        head = self.git('rev-parse', 'HEAD')
        self.accept(parent, child, head, 'false')
        child['status'] = 'in_review'
        self.comments[child['id']].append({'id': 'failed', 'created_at': '2', 'author_type': 'agent',
            'author_id': 'agent', 'content': 'Verification failed.\nCommit: ' + head})
        with self.assertRaisesRegex(ValueError, 'approved commit and child integration result'):
            self.accept(parent, child, head, action='settle')
        self.assertEqual(child['status'], 'in_review')
        self.assertEqual(self.git('rev-parse', 'candidate'), head)

    def test_group_review_waits_for_child_integration(self):
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
        self.remote()
        before = {key: dict(row['metadata']) for key, row in self.rows.items()}
        folder = SKILLS / 'multica-subissue-review'
        with patch.object(sys, 'path', [str(folder), *sys.path]):
            controller = load('native_group_review', folder / 'controller.py')
        def cli(*words, **kwargs):
            if words[:2] == ('comment', 'list'):
                rows = self.cli(*words, **kwargs)
                reply = rows[0]
                reply['content'] = 'Score: ' + ('0.42' if words[2] == 'first' else '0.55') + '\n' + reply['content']
                return rows
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
            self.assertEqual(self.rows['first']['status'], 'in_progress')
            with self.assertRaisesRegex(ValueError, 'Finish child review'):
                controller.done(args, context)
            parent_dir = self.base.checkout(self.repo, parent['metadata']['git_branch'])
            self.base.git(parent_dir, 'merge', '--no-edit', head)
            self.base.git(parent_dir, 'push', 'origin', parent['metadata']['git_branch'])
            self.integration_reply(self.rows['first'], head, head)
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
        payload = [{'resource_type': 'local_directory', 'resource_ref': {
            'local_path': str(self.repo), 'daemon_id': 'daemon'}}]
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
