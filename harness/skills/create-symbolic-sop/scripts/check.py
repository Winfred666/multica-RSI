"""Validate a package, then check its shared runtime in temporary template fixtures."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

TEMPLATE = Path(__file__).resolve().parents[1] / 'assets/template'
PACKAGE = TEMPLATE


class SymbolicProtocol(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.package = self.root / 'skill'
        shutil.copytree(TEMPLATE, self.package)
        for name in ('sop.py', 'wrapper.py', 'monitor.py'):
            shutil.copy2(PACKAGE / name, self.package / name)
        self.env = {**os.environ, 'MULTICA_TASK_ID': 'protocol-fixture',
                    'MULTICA_ISSUE_ID': 'fixture-issue', 'MULTICA_WORKSPACE_ID': 'fixture-workspace',
                    'PYTHONDONTWRITEBYTECODE': '1'}
        self.env.pop('MULTICA_TOKEN', None)

    def run_sop(self, *args, valid=True, json_output=True, cwd=None):
        result = subprocess.run([sys.executable, str(self.package / 'wrapper.py'), *args],
                                cwd=cwd or self.root, env=self.env, capture_output=True, text=True, timeout=10)
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

    def test_package_configuration_and_template_without_debug_document(self):
        self.assertEqual(self.run_sop('--validate')['status'], 'valid')
        self.assertFalse((self.package / 'DEBUG.md').exists())
        (self.package / 'config.json').write_text('{"DEBUG": "false"}')
        self.assertIn('boolean DEBUG', self.run_sop('--validate', valid=False)['error'])

    def test_bound_script_monitor_dynamic_routes_and_completion(self):
        script = self.root / 'test_solution.py'
        script.write_text('from pathlib import Path\n'
                          'def validate(): return True\n'
                          'def score(delivery): return delivery["score"]\n'
                          'def is_complete(delivery): return delivery["score"] == 1\n'
                          'if __name__ == "__main__": print(\'{"score": \' + Path("score").read_text() + \'}\')\n')
        (self.root / 'score').write_text('1')
        elsewhere = self.root / 'elsewhere'
        elsewhere.mkdir()
        initial = self.run_sop()
        self.assertEqual(initial['status'], 'initialize')
        self.assertIsInstance(initial['next_step'], list)
        self.assertTrue(all(set(option) == {'instruction', 'command'} for option in initial['next_step']))
        configured = self.run_sop('configure', '--test-script', str(script))
        self.assertEqual(configured['status'], 'variant')
        self.assertNotIn('workspace', configured['data'])
        started = self.run_sop('test', '--description', 'Measure this solution.', cwd=elsewhere)
        self.assertEqual(started['status'], 'test')
        job = Path(started['data']['job'])
        self.assertEqual(json.loads((job / 'job.json').read_text())['cwd'], str(self.root))
        result = self.run_sop('continue')
        self.assertEqual(result['status'], 'review')
        self.assertEqual(result['data']['delivery'], {'score': 1})
        self.assertEqual(self.run_sop('record', '--review', 'The measured result meets the exit condition.')['status'], 'finished')
        self.assertFalse((self.package / '.state/protocol-fixture.json').exists())
        script.write_text(script.read_text().replace('def validate(): return True', 'def validate(): return False'))
        self.run_sop()
        self.assertEqual(self.run_sop('configure', '--test-script', str(script))['status'], 'scaffold')
        self.env['MULTICA_TASK_ID'] = 'invalid-branch'
        controller = self.package / 'controller.py'
        controller.write_text(controller.read_text().replace("'variant' if baseline else 'scaffold'", "'finished'"))
        self.run_sop()
        invalid = self.run_sop('configure', '--test-script', str(script), valid=False)
        self.assertEqual(invalid['error']['code'], 'invalid_branch_output')

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
        script = self.root / 'test_solution.py'
        script.write_text('def validate(): return True\n'
                          'def score(delivery): return 1\n'
                          'def is_complete(delivery): return True\n')
        initial = self.run_sop()
        current = self.run_sop('configure', '--test-script', str(script))
        spec = importlib.util.spec_from_file_location('protocol_wrapper', self.package / 'wrapper.py')
        wrapper = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(wrapper)
        (self.package / 'config.json').write_text('{"DEBUG": true}')
        with patch.dict(sys.modules), patch.object(sys, 'path', [str(self.package), *sys.path]):
            sys.modules.pop('sop', None)
            sys.modules.pop('monitor', None)
            controller = wrapper.load_controller()
        state = json.loads((self.package / '.state/protocol-fixture.json').read_text())
        context = {'task_id': 'protocol-fixture', 'state': state, 'argv': ['configure'],
                   'prior_status': 'initialize', 'output': current}
        result = {'status': 'variant', 'state_changed': True, 'accepted': True}
        with patch.dict(os.environ, MULTICA_ISSUE_ID='fixture-issue', MULTICA_TASK_ID='protocol-fixture',
                        MULTICA_WORKSPACE_ID='fixture-workspace', MULTICA_TOKEN='fixture-token',
                        MULTICA_SERVER_URL='http://fixture.invalid'), patch.object(wrapper, 'upload_artifact') as upload:
            wrapper.publish_artifacts(context, controller, result)
        filename, html = upload.call_args.args
        self.assertEqual(filename, 'multica-parallel-refinement-state.html')
        self.assertIn('protocol-fixture', html)
        self.assertIn('initialize __option_0@--&gt;|configure| configure_result', html)
        self.assertIn('configure_result --&gt;|yes| variant', html)
        self.assertIn('configure_result --&gt;|no| scaffold', html)
        self.assertIn('__start@{ shape: sm-circ } --&gt; initialize', html)
        self.assertIn('finished --&gt; __end@{ shape: fr-circ }', html)
        self.assertLess(html.index('id="workflow"'), html.index('<h1>'))
        data = json.loads(html.split('<script id="sop-data" type="application/json">', 1)[1].split('</script>', 1)[0])
        self.assertEqual(data['nodes']['initialize'], initial)
        self.assertEqual(data['nodes']['variant'], current)
        self.assertEqual(data['edges']['__option_0']['decision'], initial['next_step'][0])
        self.assertIn('Run delivery-format check', data['edges']['__option_0']['logic']['configure'])
        overview = html.split('<pre class="mermaid" id="workflow">', 1)[1].split('</pre>', 1)[0]
        self.assertNotIn('Reject; keep current state', overview)
        self.assertNotIn('Review is one paragraph?', overview)
        self.assertNotIn('Measured delivery complete?', overview)
        self.assertIn('Reject; keep current state', data['logic']['variant']['test'])
        state['option_diagrams']['initialize']['configure'] = 'flowchart LR\n    doc_only{"Local check?"} --> doc_done'
        changed = wrapper.state_artifact(context, controller, result)
        changed_overview = changed.split('<pre class="mermaid" id="workflow">', 1)[1].split('</pre>', 1)[0]
        self.assertEqual(overview, changed_overview)
        self.assertNotIn('def configure', html)
        self.assertNotIn('__start', data['nodes'])
        self.assertNotIn('__end', data['nodes'])
        self.assertEqual(data['nodes']['finished']['next_step'], [])
        self.assertLess(html.index('id="detail"'), html.index('id="logic"'))
        self.assertLess(html.index('id="logic"'), html.index('id="metadata"'))
        self.assertNotIn('<button', html)
        self.assertNotIn('help', data)
        for name in controller.workflow.states:
            self.assertIn(name, html)


if __name__ == '__main__':
    if len(sys.argv) > 1 and not sys.argv[1].startswith('-'):
        PACKAGE = Path(sys.argv.pop(1)).resolve()
    subprocess.run([sys.executable, str(PACKAGE / 'wrapper.py'), '--validate'], check=True)
    unittest.main()
