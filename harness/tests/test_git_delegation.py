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
        with patch.object(sys, 'path', [str(SKILLS / 'multica-subissue-v2'), *sys.path]):
            self.sop = load('native_git_delegation', SKILLS / 'multica-subissue-v2/controller.py')
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
                 'metadata': props, 'labels': [{'name': tag}], 'status': 'in_progress'}
        self.rows[identity] = value
        return value

    def test_supplied_branch_reuses_its_checkout_without_path_metadata(self):
        self.git('checkout', '-qb', 'refinement')
        self.git('remote', 'add', 'origin', 'git@example.com:owner/repo.git')
        root = self.issue('root', git_branch='refinement', git_base_branch='main')
        bound = self.base.bootstrap(self.repo, root, 'workspace')
        self.assertEqual(bound['workdir'], str(self.repo))
        self.assertEqual(root['metadata'], {'git_repository': 'git@example.com:owner/repo.git',
                         'git_branch': 'refinement', 'git_base_branch': 'main'})
        self.assertTrue(self.base.owned_branch(self.repo, 'refinement', root, 'workspace'))
        other = self.issue('other', git_branch='refinement', git_base_branch='main')
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
        self.assertEqual(child['metadata']['git_base_branch'], inherited['branch'])
        self.assertEqual(set(child['metadata']), {'git_branch', 'git_base_branch'})
        self.assertEqual(set(query['metadata']), {'git_branch', 'git_base_branch'})

    def test_review_merges_the_child_through_git_branch_bindings(self):
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
        value = {'id': 'review', 'action': 'accept', 'phase': 'requested', 'parent_id': 'parent',
                 'parent_branch': parent['metadata']['git_branch'], 'child_branch': bound['branch'],
                 'head': head, 'repository': None, 'verify': 'test "$(cat solution.txt)" = accepted'}
        settle = load('native_git_settlement', SKILLS / 'multica-subissue-v2/review_settle.py')
        def cli(*words, **kwargs):
            if words[0] == 'status':
                child['status'] = words[2]
        with patch.object(self.sop, 'cli', side_effect=cli), patch.object(settle, 'save'), \
                patch.object(self.sop.delivery, 'notify', return_value={}):
            settle.settle(self.sop, self.repo, common, child, parent, value)
        parent_dir = self.base.checkout(self.repo, parent['metadata']['git_branch'])
        self.assertEqual((parent_dir / 'solution.txt').read_text(), 'accepted\n')
        self.assertEqual(child['status'], 'done')
        self.assertFalse(self.git('branch', '--list', bound['branch']))
        self.assertEqual(self.git('show', 'main:solution.txt'), 'baseline')

    def test_review_of_supplied_root_saves_no_local_path(self):
        self.issue('parent', tag='query')
        self.git('checkout', '-qb', 'candidate')
        child = self.issue('child', parent='parent', git_branch='candidate', git_base_branch='main')
        child.update(status='in_review', assignee_id='agent')
        self.base.bootstrap(self.repo, child, 'workspace')
        head = self.git('rev-parse', 'HEAD')
        folder = SKILLS / 'multica-subissue-review'
        with patch.object(sys, 'path', [str(folder), *sys.path]), patch.dict(sys.modules):
            sys.modules.pop('review_settle', None)
            core = load('native_review', folder / 'review_core.py')
            settle = sys.modules['review_settle']
        def cli(*words, **kwargs):
            if words[:2] == ('metadata', 'set'):
                child['metadata'][words[words.index('--key') + 1]] = words[words.index('--value') + 1]
            elif words[0] == 'status':
                child['status'] = words[2]
            elif words[0] == 'get':
                return self.rows[words[1]]
        args = SimpleNamespace(repo=self.repo, commit=head, verify='test -f solution.txt', reason=None)
        with patch.object(core, 'child_context', return_value=(self.rows['parent'], self.sop, child, None, None)), \
                patch.object(core, 'passed_delivery', return_value={'head': head}), patch.object(core, 'dispatch'):
            core.decide(args, {'cli': cli, 'error': lambda code, message: ValueError(message)}, 'accept')
        decision = json.loads(child['metadata']['subissue_review'])
        self.assertIn('repository', decision)
        self.assertNotIn('repo', decision)
        self.assertNotIn(str(self.root), json.dumps(decision))
        with patch.object(settle, 'branch_sop', return_value=self.sop), patch.object(self.sop, 'cli', side_effect=cli), \
                patch.object(self.sop.delivery, 'notify', return_value={}), patch.dict(os.environ,
                    MULTICA_WORKSPACE_ID='workspace', MULTICA_ISSUE_ID='child', MULTICA_AGENT_ID='agent'):
            self.assertEqual(settle.main(['--execute', '--repo', str(self.repo)]), 0)
        self.assertEqual(child['status'], 'done')
        self.assertEqual(self.git('branch', '--show-current'), 'candidate')
        self.assertTrue(self.git('branch', '--list', 'candidate'))

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
