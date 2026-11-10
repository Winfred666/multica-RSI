import json
import os
from pathlib import Path
import subprocess
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import controller


class MemberContext(unittest.TestCase):
    def context(self):
        return {'error': lambda code, message, hint=None: ValueError(code + ': ' + message),
                'state': {'target': '22222222-2222-4222-8222-222222222222'}}

    def invoke(self, token, result=None):
        read, write = os.pipe()
        self.addCleanup(os.close, read)
        os.write(write, token.encode())
        os.close(write)
        env = {**os.environ, 'MULTICA_WORKSPACE_MEMBER_FD': str(read),
               'MULTICA_TOKEN': 'mat_origin', 'MULTICA_TASK_ID': 'task',
               'MULTICA_AGENT_ID': 'origin-agent', 'MULTICA_DAEMON_PORT': '1234',
               'MULTICA_SERVER_URL': 'https://member.invalid', 'MULTICA_CLI_PATH': '/trusted/multica'}
        mock = patch.object(subprocess, 'run', return_value=result or SimpleNamespace(
            returncode=0, stdout=json.dumps({'id': 'ok', 'echo': token}), stderr=''))
        return env, mock

    def test_explicit_member_child_has_no_task_identity_or_owner_config(self):
        env, mock = self.invoke('mul_fixture')
        with patch.dict(os.environ, env, clear=True), mock as execute:
            result = controller.member(self.context(), ['project', 'list'], self.context()['state']['target'])
        self.assertEqual(result, {'id': 'ok', 'echo': '[REDACTED]'})
        args, kwargs = execute.call_args
        self.assertEqual(args[0], ['/trusted/multica', 'project', 'list', '--output', 'json'])
        child = kwargs['env']
        self.assertEqual(child['MULTICA_TOKEN'], 'mul_fixture')
        self.assertEqual(child['MULTICA_WORKSPACE_ID'], self.context()['state']['target'])
        self.assertNotIn('mul_fixture', str(args))
        self.assertNotIn('MULTICA_AGENT_ID', child)
        self.assertNotIn('MULTICA_TASK_ID', child)
        self.assertNotIn('MULTICA_DAEMON_PORT', child)
        self.assertNotIn('MULTICA_WORKSPACE_MEMBER_FD', child)
        self.assertEqual(Path(kwargs['cwd']).parent, Path.home())
        self.assertFalse(Path(kwargs['cwd']).exists())

    def test_missing_member_context_and_task_tokens_fail_closed(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(subprocess, 'run') as execute:
            with self.assertRaisesRegex(ValueError, 'member_context_required'):
                controller.member(self.context(), ['project', 'list'])
            execute.assert_not_called()
        env, mock = self.invoke('mat_origin')
        with patch.dict(os.environ, env, clear=True), mock as execute:
            with self.assertRaisesRegex(ValueError, 'member_context_required'):
                controller.member(self.context(), ['project', 'list'])
            execute.assert_not_called()

    def test_command_cannot_redirect_authorized_credential(self):
        for words in (['project', 'list', '--server-url=https://bad.invalid'],
                      ['issue', 'list', '--workspace-id', 'other'], ['config', 'show']):
            with self.subTest(words=words), patch.object(controller, 'member') as execute:
                with self.assertRaises(ValueError):
                    controller.run(SimpleNamespace(argv_json=json.dumps(words)), self.context())
                execute.assert_not_called()

    def test_failure_redacts_member_and_task_tokens(self):
        env, mock = self.invoke('mul_fixture', SimpleNamespace(returncode=1, stdout='', stderr='mul_fixture mat_origin mcn_other'))
        with patch.dict(os.environ, env, clear=True), mock:
            with self.assertRaisesRegex(ValueError, r'\[REDACTED\] \[REDACTED\] \[REDACTED\]'):
                controller.member(self.context(), ['issue', 'list'])


if __name__ == '__main__':
    unittest.main()
