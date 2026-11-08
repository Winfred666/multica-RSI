"""Real Git ownership and delegation with path-free issue metadata."""
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
