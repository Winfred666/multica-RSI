"""The portable package validates and runs the synchronized Symbolic protocol."""
import importlib.util
import inspect
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

SKILLS = Path(__file__).resolve().parents[1] / 'skills'
TEMPLATE = SKILLS / 'create-symbolic-sop/assets/template'
PACKAGES = [TEMPLATE, *(SKILLS / name for name in (
    'multica-subissue-distribute', 'multica-surface-block'))]


class SymbolicProtocol(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.package = self.root / 'skill'
        shutil.copytree(TEMPLATE, self.package)
        self.env = {**os.environ, 'MULTICA_TASK_ID': 'protocol-fixture',
                    'MULTICA_ISSUE_ID': 'fixture-issue', 'MULTICA_WORKSPACE_ID': 'fixture-workspace',
                    'PYTHONDONTWRITEBYTECODE': '1'}
        self.env.pop('MULTICA_TOKEN', None)

    def run_sop(self, *args, valid=True, json_output=True):
        result = subprocess.run([sys.executable, str(self.package / 'wrapper.py'), *args],
                                cwd=self.root, env=self.env, capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0 if valid else 1, result.stderr + result.stdout)
        return json.loads(result.stdout) if json_output else result.stdout.strip()

    def test_help_shows_body_without_starting_then_replays_same_state_output(self):
        body = (self.package / 'SKILL.md').read_text().split('---', 2)[2].strip()
        self.assertEqual(self.run_sop('--help', json_output=False), body)
        self.assertFalse((self.package / '.state').exists())
        (self.package / 'controller.py').write_text('''from pathlib import Path
from sop import Option, State, Workflow
def observe(context):
    p = Path('observations')
    p.write_text(p.read_text() + 'x' if p.exists() else 'x')
    return {'data': {'output': p.read_text()}}
workflow = Workflow(
    State('start', 'Start', initial=True, view=observe, options=(
        Option('refresh', 'start', 'Refresh output'), Option('done', 'finished', 'Finish'))),
    State('finished', 'Done', final=True))
''')
        initial = self.run_sop()
        updated = self.run_sop('refresh')
        self.assertNotEqual(initial['data'], updated['data'])
        path = self.package / '.state/protocol-fixture.json'
        saved, modified = path.read_bytes(), path.stat().st_mtime_ns
        self.assertEqual(self.run_sop('--help'), updated)
        self.assertEqual((self.root / 'observations').read_text(), 'xx')
        self.assertEqual(path.read_bytes(), saved)
        self.assertEqual(path.stat().st_mtime_ns, modified)
        self.run_sop('done')
        self.assertFalse(path.exists())
        self.assertEqual(self.run_sop('--help', json_output=False), body)
        self.assertFalse(path.exists())

    def test_required_debug_and_configuration_in_every_package(self):
        for source in PACKAGES:
            with self.subTest(package=source):
                shutil.rmtree(self.package)
                shutil.copytree(source, self.package)
                self.assertEqual(self.run_sop('--validate')['status'], 'valid')
                (self.package / 'DEBUG.md').unlink()
                self.assertIn('DEBUG.md', self.run_sop('--validate', valid=False)['error'])
                shutil.copyfile(source / 'DEBUG.md', self.package / 'DEBUG.md')
                (self.package / 'config.json').write_text('{"DEBUG": "false"}')
                self.assertIn('boolean DEBUG', self.run_sop('--validate', valid=False)['error'])

    def test_bound_script_monitor_dynamic_routes_and_completion(self):
        script = self.root / 'test_solution.py'
        script.write_text('def validate(): return True\n'
                          'def score(delivery): return delivery["score"]\n'
                          'def is_complete(delivery): return delivery["score"] == 1\n'
                          'if __name__ == "__main__": print(\'{"score": 1}\')\n')
        initial = self.run_sop()
        self.assertEqual(initial['status'], 'initialize')
        self.assertIsInstance(initial['next_step'], list)
        self.assertTrue(all(set(option) == {'instruction', 'command'} for option in initial['next_step']))
        self.assertEqual(self.run_sop('configure', '--test-script', str(script))['status'], 'variant')
        self.assertEqual(self.run_sop('test', '--description', 'Measure this solution.')['status'], 'test')
        result = self.run_sop('continue')
        self.assertEqual(result['status'], 'review')
        self.assertEqual(result['data']['delivery'], {'score': 1})
        self.assertEqual(self.run_sop('record', '--review', 'The measured result meets the exit condition.')['status'], 'finished')
        self.assertFalse((self.package / '.state/protocol-fixture.json').exists())

    def test_assertion_repair_does_not_repeat_action(self):
        (self.package / 'controller.py').write_text('''from pathlib import Path
from sop import Option, State, Workflow
def write(args, context):
    p = Path('writes'); p.write_text(p.read_text() + 'x' if p.exists() else 'x')
def check(context):
    if not Path('ready').exists(): raise AssertionError('Create ready')
workflow = Workflow(
    State('start', 'Start', initial=True, options=(Option('write', 'finished', 'Write once', write),)),
    State('finished', 'Done', final=True, precheck=check))
''')
        self.run_sop()
        failed = self.run_sop('write', valid=False)
        self.assertEqual(failed['status'], 'error')
        self.assertEqual(self.run_sop('--help'), failed)
        (self.root / 'ready').touch()
        self.assertEqual(self.run_sop('repaired')['status'], 'finished')
        self.assertEqual((self.root / 'writes').read_text(), 'x')

    def test_monitor_returns_only_appended_output(self):
        spec = importlib.util.spec_from_file_location('protocol_monitor', self.package / 'monitor.py')
        monitor = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(monitor)
        job = self.root / 'job'
        job.mkdir()
        (job / 'stdout.log').write_text('progress\n')
        (job / 'stderr.log').write_text('WARNING: inspect result\n')
        cursor = {}
        first = monitor.wait(job, cursor)
        self.assertEqual(first['reason'], 'important_log')
        (job / 'result.json').write_text('{"returncode": 0}')
        second = monitor.wait(job, cursor)
        self.assertEqual(second['reason'], 'exit')
        self.assertEqual(second['new_output'], {})

    def test_configured_preview_uses_actual_workflow(self):
        spec = importlib.util.spec_from_file_location('protocol_wrapper', self.package / 'wrapper.py')
        wrapper = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(wrapper)
        (self.package / 'config.json').write_text('{"DEBUG": true}')
        with patch.dict(sys.modules), patch.object(sys, 'path', [str(self.package), *sys.path]):
            sys.modules.pop('sop', None)
            sys.modules.pop('monitor', None)
            controller = wrapper.load_controller()
        context = {'task_id': 'fixture-task', 'state': {}, 'argv': ['configure'], 'prior_status': 'initialize'}
        result = {'status': 'variant', 'state_changed': True, 'accepted': True}
        with patch.dict(os.environ, MULTICA_ISSUE_ID='fixture-issue', MULTICA_TASK_ID='fixture-task',
                        MULTICA_WORKSPACE_ID='fixture-workspace', MULTICA_TOKEN='fixture-token',
                        MULTICA_SERVER_URL='http://fixture.invalid'), patch.object(wrapper, 'upload_artifact') as upload:
            wrapper.publish_artifacts(context, controller, result)
        filename, html = upload.call_args.args
        self.assertEqual(filename, 'multica-parallel-refinement-state.html')
        self.assertIn('fixture-task', html)
        self.assertIn('initialize --&gt;|configure| variant', html)
        self.assertLess(html.index('<pre class="mermaid">'), html.index('<h1>'))
        data = json.loads(html.split('<script id="sop-data" type="application/json">', 1)[1].split('</script>', 1)[0])
        self.assertEqual(data['edges'][0]['action'], inspect.getsource(controller.configure))
        for name in controller.workflow.states:
            self.assertIn(name, html)


if __name__ == '__main__':
    unittest.main()
