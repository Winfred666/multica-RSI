"""Shared Python engine protocol tests using CLI-owned semantic examples."""
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

DEVKIT = Path(__file__).with_name('sopdev')
PACKAGE = Path(__file__).with_name('sop')


class SymbolicProtocol(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.package = self.root / 'skill'
        self.package.mkdir()
        (self.package / 'SKILL.md').write_text((DEVKIT / 'SKILL.md').read_text().replace('{{name}}', 'fixture-sop'))
        shutil.copy2(DEVKIT / 'feature-rich.py', self.package / 'controller.py')
        self.engine = self.root / 'engine'
        shutil.copytree(PACKAGE, self.engine)
        self.state_dir = self.root / 'state'
        self.state_dir.mkdir()
        self.enterContext(patch.object(sys, 'path', [str(self.engine), str(self.package), *sys.path]))
        self.enterContext(patch.dict(sys.modules))
        for name in ('common', 'sop', 'monitor'):
            sys.modules.pop(name, None)
        self.metadata = self.root / 'metadata'
        binary = self.root / 'bin'
        binary.mkdir()
        cli = binary / 'multica'
        cli.write_text(f'#!{sys.executable}\n' + '''import json, os, sys
from pathlib import Path
path = Path(os.environ['SOP_TEST_METADATA'])
store = json.loads(path.read_text()) if path.exists() else {}
args = sys.argv[1:]
if os.environ.get('SOP_TEST_CLI_FAIL'):
    sys.exit('Metadata service unavailable')
if args[:2] == ['issue', 'get']:
    print(json.dumps({'id': args[2], 'metadata': store.get(args[2], {})}))
elif args[:2] == ['issue', 'assign']:
    print('{}')
else:
    action, issue = args[2:4]
    if os.environ.get('SOP_TEST_WRITE_FAIL'):
        sys.exit('Metadata write unavailable')
    key = args[args.index('--key') + 1]
    metadata = store.setdefault(issue, {})
    if action == 'set':
        metadata[key] = args[args.index('--value') + 1]
        if len(json.dumps(metadata).encode()) > 8192:
            sys.exit('Issue metadata exceeds 8 KB')
    elif action == 'delete':
        metadata.pop(key, None)
    else:
        sys.exit('Unexpected fixture command')
    path.write_text(json.dumps(store))
    print(json.dumps({'metadata': metadata}))
''')
        cli.chmod(0o755)
        self.env = {**os.environ, 'MULTICA_ISSUE_ID': 'fixture-issue', 'MULTICA_WORKSPACE_ID': 'fixture-workspace',
                    'SOP_TEST_METADATA': str(self.metadata), 'PATH': str(binary) + os.pathsep + os.environ['PATH'],
                    'PYTHONDONTWRITEBYTECODE': '1'}
        self.env.pop('MULTICA_TOKEN', None)

    def skill_name(self):
        import re
        return re.search(r'^name: (.+)$', (self.package / 'SKILL.md').read_text(), re.M)[1]

    def state(self):
        metadata = json.loads(self.metadata.read_text())[self.env['MULTICA_ISSUE_ID']]
        value = metadata.get('sop.' + self.skill_name())
        return json.loads(value) if value is not None else {}

    def run_sop(self, *args, valid=True, json_output=True, cwd=None):
        result = subprocess.run([sys.executable, str(self.engine / 'wrapper.py'),
                                '--sop-dir', str(self.package), '--state-dir', str(self.state_dir),
                                '--name', self.skill_name(), '--', *args],
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
    return {'instruction': 'Output: ' + p.read_text(), 'data': {'internal': 'private-view-data'}}
workflow = Workflow(
    Option('refresh', 'Refresh output'),
    State('start', 'Start', initial=True, view=observe),
    State('done', 'Done', final=True),
    edges=(('start', 'refresh'), ('refresh', 'start'), ('start', 'done')),
)
''')
        initial = self.run_sop()
        self.assertEqual(set(self.state()), {'status', 'output', 'runtime'})
        updated = self.run_sop('refresh')
        self.assertNotEqual(initial['instruction'], updated['instruction'])
        self.assertNotIn('data', updated)
        path = self.metadata
        saved = path.read_bytes()
        self.assertEqual(self.run_sop('--help'), updated)
        self.assertEqual((self.root / 'observations').read_text(), 'xx')
        self.assertEqual(path.read_bytes(), saved)
        # A remote edit must be read on the next recall, without a local cache.
        store = json.loads(saved)
        state = self.state()
        state['output']['instruction'] = 'Updated directly in issue metadata'
        store['fixture-issue']['sop.' + self.skill_name()] = json.dumps(state)
        path.write_text(json.dumps(store))
        self.assertEqual(self.run_sop('--help'), state['output'])
        self.assertEqual((self.root / 'observations').read_text(), 'xx')
        self.assertEqual(set(self.state()), {'status', 'output', 'runtime'})
        self.run_sop('done')
        self.assertFalse(self.state())
        self.assertEqual(self.run_sop('--help', json_output=False), body)
        self.assertFalse(self.state())
        self.assertFalse((self.package / '.state').exists())

    def test_package_configuration_and_template_without_debug_document(self):
        self.assertEqual(self.run_sop('--validate')['status'], 'valid')
        self.assertFalse((self.package / 'DEBUG.md').exists())
        (self.package / 'config.json').write_text('{"DEBUG": "false"}')
        self.assertIn('DEBUG must be a boolean', self.run_sop('--validate', valid=False)['error'])
        spec = importlib.util.spec_from_file_location('protocol_wrapper', self.engine / 'wrapper.py')
        wrapper = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(wrapper)
        wrapper.ROOT, wrapper.STATE_DIR, wrapper.SOP_NAME = self.package, self.state_dir, self.skill_name()
        (self.package / 'config.json').write_text('{}')
        self.assertEqual(self.run_sop('--validate')['status'], 'valid')
        self.assertFalse(wrapper.load_controller().DEBUG)
        (self.package / 'config.json').unlink()
        self.assertEqual(self.run_sop('--validate')['status'], 'valid')
        self.assertFalse(wrapper.load_controller().DEBUG)
        with patch.object(wrapper, 'publish_shared_artifact') as publish:
            wrapper.publish_artifacts({'issue_id': 'fixture-issue'}, wrapper.load_controller(),
                                      {'state_changed': True})
        publish.assert_not_called()

    def test_dependencies_install_when_missing_and_report_install_failure(self):
        spec = importlib.util.spec_from_file_location('protocol_wrapper', self.engine / 'wrapper.py')
        wrapper = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(wrapper)
        binaries = self.root / 'installers'
        binaries.mkdir()
        shutil.copy2(self.root / 'bin/multica', binaries / 'multica')
        installer = binaries / 'apt-get'
        installer.write_text(f'''#!{sys.executable}
from pathlib import Path
import sys
root = Path(__file__).parent
with (root / 'installs').open('a') as log: log.write(sys.argv[-1] + '\\n')
print('package manager output')
if (root / 'fail').exists():
    print('installation refused', file=sys.stderr)
    sys.exit(9)
package = sys.argv[sys.argv.index('--id') + 1] if '--id' in sys.argv else sys.argv[-1]
name = {{'graphviz': 'dot', 'Graphviz.Graphviz': 'dot', 'bubblewrap': 'bwrap'}}[package]
tool = root / name
tool.write_text('#!/bin/sh\\nexit 0\\n')
tool.chmod(0o755)
''')
        installer.chmod(0o755)
        sudo = binaries / 'sudo'
        sudo.write_text('#!/bin/sh\nshift\nexec "$@"\n')
        sudo.chmod(0o755)
        with patch.dict(os.environ, PATH=str(binaries)):
            for name, package in (('dot', 'graphviz'), ('bwrap', 'bubblewrap')):
                self.assertEqual(wrapper.ensure_command(name, package), str(binaries / name))
                self.assertEqual(wrapper.ensure_command(name, package), str(binaries / name))
        self.assertEqual((binaries / 'installs').read_text().splitlines(), ['graphviz', 'bubblewrap'])
        (binaries / 'dot').unlink()
        (binaries / 'fail').touch()
        self.env['PATH'] = str(binaries)
        (self.package / 'controller.py').write_text('''from sop import Option, State, Workflow
from wrapper import ensure_command
def install(args, context): ensure_command('dot', 'graphviz')
workflow = Workflow(
    Option('install', 'Install', install),
    State('start', 'Start', initial=True),
    State('finished', 'Done', final=True),
    edges=(('start', 'install'), ('install', 'finished')),
)
''')
        self.run_sop('--help', json_output=False)
        self.run_sop()
        self.assertEqual((binaries / 'installs').read_text().splitlines(), ['graphviz', 'bubblewrap'])
        failed = self.run_sop('install', valid=False)
        self.assertEqual(failed['error']['code'], 'dependency_install_failed')
        self.assertIn('installation refused', failed['error']['message'])
        self.assertIn('Resolve the installation error for graphviz', failed['instruction'])
        self.assertEqual((binaries / 'installs').read_text().splitlines(), ['graphviz', 'bubblewrap', 'graphviz'])
        self.assertEqual(failed['status'], 'start')
        (binaries / 'fail').unlink()
        installer.rename(binaries / 'winget')
        with patch.dict(os.environ, PATH=str(binaries)):
            self.assertEqual(wrapper.ensure_command('dot', 'graphviz'), str(binaries / 'dot'))
        self.assertEqual((binaries / 'installs').read_text().splitlines()[-1], 'Graphviz.Graphviz')

    def test_feature_rich_handoff_and_same_agent_continuation(self):
        controller = self.package / 'controller.py'
        controller.write_text(controller.read_text() + "\nworkflow.states['ready'].options = lambda context: ('submit',)\n")
        self.env['MULTICA_AGENT_ID'] = 'author'
        initial = self.run_sop()
        self.assertEqual(initial['status'], 'ready')
        self.assertIsInstance(initial['next_step'], list)
        self.assertTrue(all(set(option) == {'instruction', 'command'} for option in initial['next_step']))
        request = ('submit', '--reviewer-id', 'peer', '--reviewer-name', 'Mara', '--summary', 'Check the change')
        current = self.run_sop(*request)
        self.assertEqual(current['status'], 'review')
        self.assertEqual(set(current), {'status', 'instruction'})
        self.assertIn('[@Mara](mention://agent/peer)', current['instruction'])
        self.assertIn('Please review: Check the change.', current['instruction'])
        self.assertIn('end this turn; do not choose for the reviewer', current['instruction'])
        self.env['MULTICA_AGENT_ID'] = 'peer'
        resumed = self.run_sop('--help')
        self.assertIn('Review: Check the change.', resumed['instruction'])
        self.assertTrue(resumed['next_step'][0]['command'].endswith(' accept'))
        completed = self.run_sop('accept')
        self.assertEqual(completed['status'], 'FINISHED')
        self.assertNotIn('data', completed)
        self.assertFalse(self.state())
        self.run_sop()
        continued = self.run_sop(*request)
        self.assertIn('Review: Check the change.', continued['instruction'])
        self.assertTrue(continued['next_step'][0]['command'].endswith(' accept'))
        self.run_sop('accept')
        self.assertFalse(self.state())

    def test_selected_and_branch_action_inputs_convert_once(self):
        from argparse import Namespace
        from sop import Branch, FINISHED, Option, Parameter, State, Workflow
        payload = Parameter('--payload', type=json.loads, default='{"ready": true}')
        workflow = Workflow(
            State('ready', 'Ready', initial=True),
            Option('parse', action=lambda args, context: {'data': args.payload}, params=(payload,)),
            Branch('route', 'Route', select=lambda args, context: 'parse'),
            edges=(('ready', 'parse'), ('ready', 'route'), ('route', 'parse'), ('parse', FINISHED)))
        for event in ('parse', 'route'):
            args = Namespace(event=event, extra=['--payload', '{"ready": true}'] if event == 'parse' else [])
            self.assertEqual(workflow.step(args, {'error': RuntimeError}, 'ready')['data'], {'ready': True})

    def test_assertion_repair_does_not_repeat_action(self):
        (self.package / 'controller.py').write_text('''from pathlib import Path
from sop import Option, State, Workflow
def write(args, context):
    p = Path('writes'); p.write_text(p.read_text() + 'x' if p.exists() else 'x')
    return {'data': {'internal': 'private-action-data'}}
def check(context):
    if context['result']['data'] != {'internal': 'private-action-data'}: raise AssertionError('Lost internal result')
    if not Path('ready').exists(): raise AssertionError('Create ready')
workflow = Workflow(
    Option('write', 'Write once', write),
    State('start', 'Start', initial=True),
    State('finished', 'Done', final=True, precheck=check),
    edges=(('start', 'write'), ('write', 'finished')),
)
''')
        self.run_sop()
        failed = self.run_sop('write', valid=False)
        self.assertEqual(failed['status'], 'error')
        self.assertNotIn('data', failed)
        self.assertNotIn('private-action-data', json.dumps(failed))
        saved = self.state()
        self.assertEqual(saved['recovery']['assertion']['result']['data'], {'internal': 'private-action-data'})
        self.assertNotIn('task_id', json.dumps(self.state()))
        self.assertFalse((self.package / '.errors').exists())
        self.assertEqual(self.run_sop('--help'), failed)
        self.env['MULTICA_AGENT_ID'] = 'another-agent'
        (self.root / 'ready').touch()
        completed = self.run_sop('repaired')
        self.assertEqual(completed['status'], 'finished')
        self.assertNotIn('data', completed)
        self.assertEqual((self.root / 'writes').read_text(), 'x')
        (self.root / 'ready').unlink()
        self.run_sop()
        self.run_sop('write', valid=False)
        self.assertEqual(self.run_sop('repaired', valid=False)['status'], 'blocked')
        self.assertEqual(self.run_sop(valid=False)['status'], 'blocked')
        self.assertEqual((self.root / 'writes').read_text(), 'xx')

    def test_issue_sop_isolation_handoff_and_quit(self):
        initial = self.run_sop()
        store = json.loads(self.metadata.read_text())
        store['fixture-issue']['unrelated'] = 'preserve'
        self.metadata.write_text(json.dumps(store))
        other = self.root / 'other-machine' / 'skill'
        shutil.copytree(self.package, other)
        self.package = other
        self.env['MULTICA_AGENT_ID'] = 'receiving-agent'
        replay = self.run_sop('--help')
        self.assertEqual(replay['status'], initial['status'])
        self.assertTrue(replay['next_step'][0]['command'].startswith('multica sop run fixture-sop '))
        self.assertNotIn(str(self.root / 'skill'), replay['next_step'][0]['command'])
        # Help must not import the receiving controller or write metadata.
        controller = self.package / 'controller.py'
        source = controller.read_text()
        controller.write_text('raise RuntimeError("must not load")\n')
        saved = self.metadata.read_bytes()
        self.assertEqual(self.run_sop('--help'), replay)
        self.assertEqual(self.metadata.read_bytes(), saved)
        controller.write_text(source)
        self.assertEqual(self.run_sop()['status'], initial['status'])
        skill = self.package / 'SKILL.md'
        original = skill.read_text()
        skill.write_text(original.replace('fixture-sop', 'other-sop'))
        self.run_sop()
        metadata = json.loads(self.metadata.read_text())['fixture-issue']
        self.assertIn('sop.other-sop', metadata)
        self.assertIn('sop.fixture-sop', metadata)
        skill.write_text(original)
        self.env['MULTICA_ISSUE_ID'] = 'another-issue'
        body = skill.read_text().split('---', 2)[2].strip()
        self.assertEqual(self.run_sop('--help', json_output=False), body)
        self.run_sop()
        self.run_sop('quit', valid=False)
        self.assertFalse(self.state())
        self.env['MULTICA_ISSUE_ID'] = 'fixture-issue'
        self.run_sop('quit', valid=False)
        metadata = json.loads(self.metadata.read_text())['fixture-issue']
        self.assertEqual(metadata['unrelated'], 'preserve')
        self.assertIn('sop.other-sop', metadata)
        self.assertNotIn('sop.fixture-sop', metadata)
        self.assertFalse(list(self.root.rglob('.state')))
        self.assertFalse(list(self.root.rglob('.errors')))

    def test_graph_rejects_action_cycles_and_every_dead_end_branch(self):
        from sop import Branch, Option, State, Workflow
        nodes = (State('inspect', 'Inspect.', initial=True), Option('check', 'Check.'),
      Branch('route', 'Route.'), State('review', 'Review.'), Option('dead', 'Dead.'))
        edges = (('inspect', 'check'), ('check', 'route'), ('route', 'review'), ('review', 'inspect'))
        with self.assertRaisesRegex(ValueError, 'dead'):
            Workflow(*nodes, edges=edges)
        with self.assertRaisesRegex(ValueError, 'inspect -> check -> route -> dead'):
            Workflow(*nodes, edges=(*edges, ('route', 'dead')))
        with self.assertRaisesRegex(ValueError, 'inspect -> check -> route -> check'):
            Workflow(*nodes[:-1], edges=(*edges, ('route', 'check')))
        workflow = Workflow(*nodes[:-1], edges=edges)
        self.assertEqual(workflow.graph()[1], edges)
        state = workflow.states['inspect']
        state.options = lambda context: self.fail('Validation must not execute factories')
        workflow.validate_graph()
        self.assertEqual(workflow.graph()[1], edges)
        state.options = lambda context: ('missing',)
        with self.assertRaisesRegex(ValueError, 'declared destination'):
            workflow.choices(state, {})
        with self.assertRaisesRegex(ValueError, 'Edges must be .* pairs'):
            Workflow(*nodes[:-1], edges=(('inspect', 'label', 'check'),))

    def test_addressed_entry_validation_requires_guidance_on_final_action(self):
        source = """from pathlib import Path
from sop import Branch, FINISHED, Option, State, Workflow
def earlier(args, context):
    Path('calls').touch()
    return {'this_round_reply_guidance': 'Earlier is not sufficient.'}
def final(args, context):
    Path('calls').touch()
    return FINAL_RESULT
workflow = Workflow(
    Option('ask', 'Ask.', earlier),
    State('ready', 'Prepare.', initial=True, owner={'id': 'host', 'name': 'Host'}),
    Option('last', 'Last action.', final),
    Branch('route', 'Choose.'),
    Option('finish', 'Finish.'),
    State('addressed', 'Question.', owner={'issue_id': 'destination'}),
    edges=(
        ('ready', 'ask'),
        ('ask', 'last'),
        ('last', 'route'),
        ('route', 'addressed'),
        ('addressed', 'finish'),
        ('finish', FINISHED),
    ),
)
"""
        controller = self.package / 'controller.py'
        controller.write_text(source.replace('FINAL_RESULT', '{}'))
        self.assertIn('Final action into addressed state addressed', self.run_sop('--validate', valid=False)['error'])
        controller.write_text(source.replace('FINAL_RESULT', "{'this_round_reply_guidance': 'End this turn.'}"))
        self.assertEqual(self.run_sop('--validate'), {'status': 'valid'})
        controller.write_text(source.replace('FINAL_RESULT', '{}').replace("'Last action.', final", "'Last action.'"))
        self.assertIn('this_round_reply_guidance', self.run_sop('--validate', valid=False)['error'])
        self.assertFalse(self.metadata.exists())
        self.assertFalse((self.root / 'calls').exists())

    def test_addressed_questionnaire_linkage(self):
        spec = importlib.util.spec_from_file_location('protocol_wrapper', self.engine / 'wrapper.py')
        wrapper = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(wrapper)
        wrapper.ROOT, wrapper.STATE_DIR, wrapper.SOP_NAME = self.package, self.state_dir, self.skill_name()
        controller = wrapper.load_controller()
        for destination in ('source', 'destination'):
            with self.subTest(destination=destination):
                owner = {'issue_id': destination, 'thread': 'target-thread', 'id': 'reader', 'content': '/note'}
                context = {'issue_id': 'source', 'workspace_id': 'workspace', 'state': {'reply_parent': 'source-thread'}}
                rendered = {'status': 'review', 'instruction': 'Choose.', 'next_step': [
                    {'instruction': 'Accept.', 'command': 'multica sop run fixture-sop accept'}]}
                writes = []

                def post(*words):
                    writes.append((words, Path(words[words.index('--content-file') + 1]).read_text()))
                    return {'id': 'question' if len(writes) == 1 else 'forward', 'revision': 1}

                with patch.object(wrapper, 'state_owner', return_value=owner), \
                     patch.object(wrapper, 'multica', side_effect=post), \
                     patch.dict(os.environ, MULTICA_APP_URL='https://multica.example'):
                    wrapper.render_addressed_state(context, controller,
                                                   {'ok': True, 'state_changed': True, 'status': 'review'}, rendered)
                self.assertEqual(writes[0][0][:3], ('comment', 'add', 'source'))
                self.assertEqual(writes[0][0][-2:], ('--parent', 'source-thread'))
                self.assertEqual(writes[-1][0][:5], ('comment', 'update', 'question', '--expected-revision', '1'))
                question = json.loads(writes[-1][1].split('\n', 1)[1])
                self.assertEqual(question['que_comment_id'], 'question')
                self.assertTrue(question['next_step'][0]['command'].endswith('--issue source'))
                self.assertIn("multica issue comment add source --parent '<this-comment-id>'", question['how_to_step'])
                self.assertNotIn('how_to_step', rendered)
                self.assertEqual(len(writes), 2 if destination == 'source' else 3)
                if destination != 'source':
                    self.assertEqual(question['forwarded_comment_id'], 'forward')
                    self.assertEqual(writes[1][0][:5], ('comment', 'add', destination, '--parent', 'target-thread'))
                    self.assertEqual(writes[1][1], '/note\n\n[Question](https://multica.example/workspace/issues/source#comment-question)')
                else:
                    self.assertNotIn('forwarded_comment_id', question)

    def test_rejected_choice_does_not_report_remote_success(self):
        (self.package / 'controller.py').write_text("""from sop import Option, State, Workflow
def reject(args, context):
    return {'accepted': False, 'instruction': 'Not accepted.'}
workflow = Workflow(
    Option('reject', 'Reject.', reject),
    State('ready', 'Choose.', initial=True),
    edges=(('ready', 'reject'), ('reject', 'ready')),
)
""")
        self.run_sop()
        self.assertEqual(self.run_sop('reject', valid=False)['status'], 'ready')

    def test_metadata_failures_preserve_saved_state(self):
        self.run_sop()
        saved = self.metadata.read_bytes()
        self.env['SOP_TEST_CLI_FAIL'] = '1'
        self.assertEqual(self.run_sop('--help', valid=False)['error']['code'], 'cli_error')
        self.assertEqual(self.run_sop(valid=False)['error']['code'], 'cli_error')
        self.env.pop('SOP_TEST_CLI_FAIL')
        self.env['SOP_TEST_WRITE_FAIL'] = '1'
        self.assertEqual(self.run_sop(valid=False)['error']['code'], 'state_write_failed')
        self.assertEqual(self.run_sop('quit', valid=False)['error']['code'], 'state_write_failed')
        self.assertEqual(self.metadata.read_bytes(), saved)
        self.env.pop('SOP_TEST_WRITE_FAIL')
        store = json.loads(saved)
        store['fixture-issue']['sop.fixture-sop'] = '{broken'
        self.metadata.write_text(json.dumps(store))
        self.assertEqual(self.run_sop(valid=False)['error']['code'], 'invalid_state')
        self.assertEqual(json.loads(self.metadata.read_text()), store)

    def test_monitor_returns_only_appended_output(self):
        spec = importlib.util.spec_from_file_location('protocol_monitor', self.engine / 'monitor.py')
        monitor = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(monitor)
        (self.root / 'score').write_text('1')
        script = "from pathlib import Path; print('{\"score\": ' + Path('score').read_text() + '}')"
        experiment = Path(monitor.start([sys.executable, '-c', script], self.root, self.state_dir))
        result = monitor.wait(experiment, {})
        self.assertEqual(result['state'], 'ended')
        self.assertEqual(result['returncode'], 0)
        self.assertEqual(monitor.delivery(experiment), {'score': 1})
        self.assertEqual(json.loads((experiment / 'job.json').read_text())['cwd'], str(self.root))
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

    def test_config_payload_is_uploaded_and_returned_id_is_shared(self):
        import common
        reference = {'issue_id': 'installer', 'filename': 'master.json'}
        uploads = []

        def request(path, body, content_type):
            uploads.append(body)
            return {'id': 'first' if len(uploads) == 1 else 'replacement', 'markdown_url': '/stable'}

        with patch.object(common, 'request', side_effect=request), patch.object(common, 'cli', return_value={'id': 'comment'}) as cli:
            self.assertEqual(common.write_config(reference, {'setting': 1}, 'thread'), {'id': 'comment'})
            self.assertEqual(reference['attachment_id'], 'first')
            common.write_config(reference, {'setting': 2}, 'thread')
            self.assertEqual(reference['attachment_id'], 'replacement')
            self.assertNotIn('--attachment', cli.call_args.args)
            common.link_config('MAIN', reference)
            common.link_config('developer', reference, common.DELEGATION_KEY)
            common.link_config('reviewer', reference, common.DELEGATION_KEY)
            for call in cli.call_args_list[-3:]:
                value = json.loads(call.args[call.args.index('--value') + 1])
                self.assertEqual(value['attachment_id'], 'replacement')
                self.assertNotIn('setting', value)
            cli.side_effect = ValueError('Announcement unavailable')
            failed = {'issue_id': 'installer', 'filename': 'master.json'}
            with self.assertRaisesRegex(ValueError, 'Resource uploaded as replacement'):
                common.write_config(failed, {'setting': 3})
            self.assertEqual(failed['attachment_id'], 'replacement')
        self.assertNotIn(b'name="attachment_id"', uploads[0])
        self.assertIn(b'name="attachment_id"\r\n\r\nfirst', uploads[1])
        for body in uploads:
            self.assertIn(b'Content-Type: application/json', body)
            for name in ('independent', 'issue_id', 'comment_id', 'task_id'):
                self.assertNotIn(('name="' + name + '"').encode(), body)

    def test_configured_preview_uses_pair_declarations_and_resolved_output(self):
        (self.package / 'controller.py').write_text('''from sop import Branch, FINISHED, Option, Parameter, State, Workflow
def publish(args, context):
    context['state']['note'] = args.note
    return {'this_round_reply_guidance': 'Published ' + args.note + '.'}
def review(context):
    return {'instruction': 'Review ' + context['state'].get('note', 'draft') + '.'}
workflow = Workflow(
    State('start', 'Choose <literal>.', initial=True),
    Option('publish', 'Publish.', publish, params=(Parameter('--note', 'Note'),),
           this_round_reply_guidance_preview='Published {note}.'),
    Branch('route', 'Select review.', select=lambda args, context: 'review'),
    State('review', 'Review the publication.', view=review, preview='Review {note}.'),
    State('complete', 'Complete the review.', final=True),
    edges=(('start', 'publish'), ('start', 'review'), ('start', 'route'), ('start', FINISHED),
           ('publish', 'route'), ('route', 'review'), ('review', 'complete')))
''')
        initial = self.run_sop()
        current = self.run_sop('publish', '--note', 'report')
        self.assertEqual(current, {'status': 'review', 'instruction': 'Published report.'})
        resumed = self.run_sop('--help')
        self.assertIn('Review report.', resumed['instruction'])
        self.assertEqual(resumed['next_step'][0]['command'], 'multica sop run fixture-sop complete')
        spec = importlib.util.spec_from_file_location('protocol_wrapper', self.engine / 'wrapper.py')
        wrapper = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(wrapper)
        wrapper.ROOT, wrapper.STATE_DIR, wrapper.SOP_NAME = self.package, self.state_dir, self.skill_name()
        (self.package / 'config.json').write_text('{"DEBUG": true}')
        controller = wrapper.load_controller()
        def forbidden(*args):
            self.fail('Static compilation evaluated a runtime hook')
        workflow = controller.workflow
        workflow.options['publish'].action = workflow.branches['route'].select = forbidden
        for state in workflow.states.values():
            state.precheck = state.owner = state.options = forbidden
            if state.view:
                state.view = forbidden
        preview = wrapper.compile_preview(controller)
        self.assertEqual(set(preview['nodes']), {'start', 'publish', 'route', 'review', 'complete', 'FINISHED'})
        self.assertEqual(preview['edges'], [list(pair) for pair in workflow.graph()[1]])
        self.assertEqual(preview['nodes']['start']['instruction'], 'Choose <literal>.')
        self.assertEqual(preview['nodes']['review']['instruction'], 'Review {note}.')
        self.assertEqual([{k: v for k, v in choice.items() if k != 'node'}
                          for choice in preview['nodes']['start']['next_step']], initial['next_step'][:-1])
        self.assertEqual(preview['nodes']['complete']['next_step'], [])
        self.assertNotIn('next_step', preview['nodes']['FINISHED'])
        self.assertEqual(preview['nodes']['publish']['destinations'], ['route'])
        self.assertEqual(preview['nodes']['route']['destinations'], ['review'])
        self.assertEqual(preview['nodes']['publish']['parameters'][0]['flag'], '--note')
        self.assertEqual(preview['nodes']['publish']['this_round_reply_guidance'], 'Published {note}.')
        self.assertTrue(all('target' not in node and 'routes' not in node for node in preview['nodes'].values()))
        workflow.states['review'].preview = None
        with self.assertRaisesRegex(ValueError, 'State review.*explicit static preview'):
            wrapper.compile_preview(controller)
        workflow.states['review'].preview = 'Review {note}.'
        context = {'issue_id': 'fixture-issue', 'argv': ['publish', '--note', 'report'],
                   'output': {**resumed, 'workspace': 'private-workspace', 'data': {'private': True}},
                   'action_outputs': {'publish': {'this_round_reply_guidance': 'Published report.'}}}
        with patch.object(wrapper, 'publish_shared_artifact') as upload:
            wrapper.publish_artifacts(context, controller, {'status': 'review', 'state_changed': True})
        self.assertEqual(json.loads(upload.call_args_list[0].args[1]), {'DEBUG': True})
        reference, html = upload.call_args.args
        self.assertEqual(reference['filename'], 'fixture-sop-state.html')
        data = json.loads(html.split('<script id="sop-data" type="application/json">', 1)[1].split('</script>', 1)[0])
        self.assertEqual(data['template'], preview)
        self.assertEqual(data['current'], resumed)
        self.assertEqual(data['actions']['publish'], {'this_round_reply_guidance': 'Published report.'})
        self.assertTrue(all('data' not in node for node in preview['nodes'].values()))
        self.assertNotIn('private-workspace', html)
        self.assertNotIn('<svg', json.dumps(data))
        self.assertTrue(all('this_round_reply_guidance' not in choice
                            for node in preview['nodes'].values() for choice in node.get('next_step', [])))
        for name in preview['nodes']:
            self.assertEqual(html.count('id="state_' + name + '"'), 1)
        overview = html.split('<div class="diagram" id="workflow">', 1)[1].split('</div>', 1)[0]
        for edge in overview.split('class="edge"')[1:]:
            self.assertNotIn('<text', edge.split('</g>', 1)[0])
        self.assertNotIn('data.edges', html)
        self.assertNotIn('chart_', html)
        self.assertNotIn('<script src=', html)
        self.assertEqual(self.run_sop('complete')['status'], 'complete')
        with patch.object(wrapper.shutil, 'which', return_value=None):
            with self.assertRaisesRegex(wrapper.SOPError, 'no supported package manager'):
                wrapper.graph_svg('digraph { a -> b; }', 'missing_tool')
        with self.assertRaisesRegex(ValueError, 'Invalid DOT diagram'):
            wrapper.graph_svg('digraph { broken -> }', 'invalid_dot')


if __name__ == '__main__':
    unittest.main()
