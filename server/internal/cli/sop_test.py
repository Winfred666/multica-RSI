"""Shared Python engine protocol tests using CLI-owned semantic examples."""
import importlib.util
import io
from html import unescape
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
    State('start', 'Start', initial=True, view=observe, options=(
        Option('refresh', 'start', 'Refresh output'), Option('done', 'finished', 'Finish'))),
    State('finished', 'Done', final=True))
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
    State('start', 'Start', initial=True, options=(Option('install', 'finished', 'Install', install),)),
    State('finished', 'Done', final=True))
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

    def test_dynamic_options_branches_and_completion(self):
        controller = self.package / 'controller.py'
        controller.write_text(controller.read_text() + "\nchoices = workflow.states['inspect'].options\n"
                              "workflow.states['inspect'].options = lambda context: choices\n")
        sample = self.root / 'sample.txt'
        sample.touch()
        initial = self.run_sop()
        self.assertEqual(initial['status'], 'inspect')
        self.assertIsInstance(initial['next_step'], list)
        self.assertTrue(all(set(option) == {'instruction', 'command'} for option in initial['next_step']))
        empty = self.run_sop('check', '--path', sample.name)
        self.assertEqual(empty['status'], 'inspect')
        sample.write_text('Ready for review.')
        current = self.run_sop('check', '--path', sample.name)
        self.assertEqual(current['status'], 'review')
        self.assertNotIn('data', current)
        self.assertEqual(self.state()['path'], str(sample))
        self.assertNotIn('workspace', self.state())
        self.assertEqual(self.run_sop('retry')['status'], 'inspect')
        self.run_sop('check', '--path', sample.name)
        completed = self.run_sop('accept')
        self.assertEqual(completed['status'], 'FINISHED')
        self.assertNotIn('data', completed)
        self.assertFalse(self.state())
        controller.write_text(controller.read_text().replace("'review' if size else 'inspect'", "FINISHED"))
        self.run_sop()
        invalid = self.run_sop('check', '--path', sample.name, valid=False)
        self.assertEqual(invalid['error']['code'], 'invalid_branch_output')

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
    State('start', 'Start', initial=True, options=(Option('write', 'finished', 'Write once', write),)),
    State('finished', 'Done', final=True, precheck=check))
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
        self.assertEqual(self.run_sop()['status'], 'inspect')
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
    State('ready', 'Prepare.', initial=True, owner={'id': 'host', 'name': 'Host'},
          options=(Option('ask', 'last', 'Ask.', earlier),)),
    Option('last', 'route', 'Last action.', final),
    Branch('route', 'Choose.', {'go': 'addressed'}),
    State('addressed', 'Question.', owner={'issue_id': 'destination'},
          options=(Option('finish', FINISHED, 'Finish.'),)))
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

    def test_configured_preview_uses_actual_workflow(self):
        controller_path = self.package / 'controller.py'
        source = controller_path.read_text().replace(
            "return {'phase': 'review' if size else 'inspect'}",
            "return {'phase': 'review' if size else 'inspect', 'this_round_reply_guidance': 'Reply once.'}")
        controller_path.write_text(source + "\nworkflow.states['review'].owner = {'id': 'reviewer', 'name': 'Reviewer'}\n")
        self.env['MULTICA_AGENT_ID'] = 'previous-owner'
        sample = self.root / 'sample.txt'
        sample.write_text('Ready for review.')
        initial = self.run_sop()
        current = self.run_sop('check', '--path', sample.name)
        self.assertEqual(current, {'status': 'review', 'instruction': 'Reply once.'})
        self.run_sop(valid=False, json_output=False)  # The previous caller is no longer the state owner.
        self.env['MULTICA_AGENT_ID'] = 'reviewer'
        resumed = self.run_sop('--help')
        self.assertIn('Review ' + str(sample), resumed['instruction'])
        self.assertEqual(resumed['next_step'][0]['instruction'], 'Accept and complete')
        self.assertEqual(self.run_sop(), resumed)
        spec = importlib.util.spec_from_file_location('protocol_wrapper', self.engine / 'wrapper.py')
        wrapper = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(wrapper)
        wrapper.ROOT, wrapper.STATE_DIR, wrapper.SOP_NAME = self.package, self.state_dir, self.skill_name()
        (self.package / 'config.json').write_text('{"DEBUG": true}')
        with patch.dict(sys.modules), patch.object(sys, 'path', [str(self.engine), str(self.package), *sys.path]):
            sys.modules.pop('sop', None)
            sys.modules.pop('monitor', None)
            controller = wrapper.load_controller()
        state = self.state()
        context = {'issue_id': 'fixture-issue', 'skill': 'fixture-sop', 'state': state, 'argv': ['check', '--path', sample.name],
                   'prior_status': 'inspect', 'output': {**current, 'workspace': 'private-workspace'}}
        result = {'status': 'review', 'state_changed': True, 'accepted': True}
        with patch.dict(os.environ, MULTICA_ISSUE_ID='fixture-issue',
                        MULTICA_WORKSPACE_ID='fixture-workspace', MULTICA_TOKEN='fixture-token',
                        MULTICA_SERVER_URL='http://fixture.invalid'), patch.object(wrapper, 'publish_shared_artifact') as upload:
            wrapper.publish_artifacts(context, controller, result)
        config_reference, config = upload.call_args_list[0].args
        self.assertEqual(config_reference['filename'], 'fixture-sop-config.json')
        self.assertEqual(json.loads(config), {'DEBUG': True})
        reference, html = upload.call_args.args
        self.assertEqual(reference['issue_id'], 'fixture-issue')
        filename = reference['filename']
        self.assertEqual(filename, 'fixture-sop-state.html')
        self.assertIn('fixture-issue', html)
        self.assertNotIn('task_id', html)
        self.assertIn('inspect->route', unescape(html))
        self.assertIn('route->review', unescape(html))
        self.assertIn('route->inspect', unescape(html))
        self.assertIn('__start->inspect', unescape(html))
        self.assertIn('review->__end', unescape(html))
        self.assertLess(html.index('id="workflow"'), html.index('<h1>'))
        data = json.loads(html.split('<script id="sop-data" type="application/json">', 1)[1].split('</script>', 1)[0])
        self.assertEqual(data['nodes']['inspect']['next_step'], initial['next_step'])
        self.assertIn(controller.workflow.states['inspect'].description, data['nodes']['inspect']['instruction'])
        self.assertEqual(data['nodes']['review'], current)
        self.assertEqual(data['decision'], current)
        review_edge = next(edge for edge in data['edges'].values() if edge['source'] == 'review')
        self.assertEqual(review_edge['decision'], resumed['next_step'][0])
        self.assertTrue(all('data' not in decision for decision in data['nodes'].values()))
        self.assertNotIn('private-legacy-data', html)
        self.assertNotIn('private-workspace', html)
        self.assertEqual(data['edges']['option_0']['decision'], initial['next_step'][0])
        self.assertEqual(data['edges']['option_0']['source'], 'inspect')
        self.assertEqual(data['edges']['option_0']['option'], 'check')
        self.assertIn('Read file size', unescape(html))
        self.assertNotIn('<svg', json.dumps(data))
        for charts in data['logic'].values():
            for template in charts.values():
                self.assertEqual(html.count(f'<template id="{template}">'), 1)
        overview = html.split('<div class="diagram" id="workflow">', 1)[1].split('</div>', 1)[0]
        self.assertNotIn('Read file size', overview)
        local = html.split(f'<template id="{data["logic"]["inspect"]["check"]}">', 1)[1].split('</template>', 1)[0]
        self.assertIn('Read file size', local)
        controller.workflow.states['inspect'].options[0].action.__doc__ = ('```dot\n'
            'digraph local { check [label="Local check?", shape=diamond]; check -> done; }\n```')
        changed = wrapper.state_artifact(context, controller, result)
        changed_overview = changed.split('<div class="diagram" id="workflow">', 1)[1].split('</div>', 1)[0]
        self.assertEqual(overview, changed_overview)
        self.assertNotIn('def inspect_file', html)
        self.assertNotIn('__start', data['nodes'])
        self.assertNotIn('__end', data['nodes'])
        self.assertEqual(data['nodes']['FINISHED']['status'], 'FINISHED')
        self.assertLess(html.index('id="detail"'), html.index('id="logic"'))
        self.assertLess(html.index('id="logic"'), html.index('id="metadata"'))
        self.assertNotIn('<button', html)
        self.assertNotIn('<script src=', html)
        self.assertNotIn('mermaid', html)
        with patch.object(wrapper.shutil, 'which', return_value=None):
            with self.assertRaisesRegex(wrapper.SOPError, 'no supported package manager'):
                wrapper.graph_svg('digraph { a -> b; }', 'missing_tool')
        with self.assertRaisesRegex(ValueError, 'Invalid DOT diagram'):
            wrapper.graph_svg('digraph { broken -> }', 'invalid_dot')
        self.assertNotIn('help', data)
        for name in controller.workflow.states:
            self.assertIn(name, html)


if __name__ == '__main__':
    unittest.main()
