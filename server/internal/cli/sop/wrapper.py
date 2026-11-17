#!/usr/bin/env python3
"""CLI-owned symbolic SOP runtime; skills supply controller.py and optional config.json."""

import argparse
from html import escape
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
from types import SimpleNamespace

from common import cli, comment, publish_shared_artifact, request, write_artifact
import sop
import monitor


ROOT = None
STATE_DIR = None
SOP_NAME = None


def runtime_command():
    return ["multica", "sop", "run", SOP_NAME]
RULES = {
    "identity": "MULTICA_ISSUE_ID and MULTICA_WORKSPACE_ID identify the shared issue state.",
    "state": "Each SOP owns one issue metadata key, sop.<skill-name>, containing serialized JSON.",
    "transitions": "Controller phases advance on callbacks; wrapper assertions can enter error or blocked; completion deletes only this SOP's metadata key.",
    "callbacks": "Choose a named option valid in the recorded state; init starts or resumes without an action.",
    "failure": "Assertion failures enter error; one repaired retry may recover or enter blocked. Reconcile external writes first.",
    "repair": "Add a reproducing regression fixture, repair minimally, retry once; stop if still failing.",
}
PATTERNS = (
    (r"(?i)permission denied|read-only file system", "Check access to the named path; preserve its contents."),
    (r"(?i)no such file|not found|command not found", "Correct the missing path or executable, then retry once."),
    (r"(?i)conflict|unmerged", "Resolve the reported conflict and rerun the same action once."),
    (r"(?i)timed? out|connection|rate limit", "Check connectivity or the stated rate-limit reset, then retry once."),
)


class SOPError(Exception):
    def __init__(self, code, message, hint=None):
        super().__init__(message)
        self.code, self.hint = code, hint


def ensure_command(name, package):
    executable = shutil.which(name)
    if executable:
        return executable
    hint = f'Resolve the installation error for {package}, then retry this SOP call.'
    installers = (
        ('apt-get', ['install', '-y', '--no-install-recommends']),
        ('dnf', ['install', '-y']), ('yum', ['install', '-y']),
        ('apk', ['add']), ('pacman', ['-S', '--noconfirm']),
        ('brew', ['install']),
        ('winget', ['install', '--exact', '--silent',
                    '--accept-package-agreements', '--accept-source-agreements', '--id']),
    )
    for manager, arguments in installers:
        installer = shutil.which(manager)
        if not installer:
            continue
        target = {'graphviz': 'Graphviz.Graphviz'}.get(package, package) if manager == 'winget' else package
        command = [installer, *arguments, target]
        if manager not in ('brew', 'winget') and os.geteuid() != 0:
            command = ['sudo', '-n', *command]
        try:
            result = subprocess.run(command, text=True, capture_output=True, timeout=300)
        except (OSError, subprocess.TimeoutExpired) as error:
            raise SOPError('dependency_install_failed', f'Could not install {package}: {error}', hint) from error
        if result.returncode:
            raise SOPError('dependency_install_failed',
                           f'Could not install {package}: {result.stderr.strip() or result.stdout.strip()}', hint)
        executable = shutil.which(name)
        if not executable:
            raise SOPError('dependency_install_failed', f'Installed {package}, but {name} is unavailable on PATH.', hint)
        return executable
    raise SOPError('dependency_install_failed', f'Cannot install {package}: no supported package manager on PATH.', hint)


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise SOPError("invalid_input", message, "Use --help for JSON command guidance; correct the input.")


def arguments(argv, controller):
    parser = Parser(add_help=False, allow_abbrev=False)
    action = parser.add_mutually_exclusive_group()
    action.add_argument("event", nargs="?", default="init")
    action.add_argument("--error")
    action.add_argument("--error-file", type=Path)
    action.add_argument("--help", action="store_true")
    controller.workflow.configure(parser)
    if '--issue' not in parser._option_string_actions:
        parser.add_argument("--issue")
    args, args_extra = parser.parse_known_args(argv)
    args.extra = args_extra
    return args


def callback(instruction, args=None):
    if args is None:
        return {"instruction": instruction}
    argv = [*runtime_command(), *args]
    return {"instruction": instruction, "command": shlex.join(argv)}


def validate_package():
    required = ('SKILL.md', 'controller.py')
    for name in required:
        path = ROOT / name
        if not path.is_file() or not path.read_text().strip():
            raise ValueError('Incomplete symbolic SOP: ' + name)
        if name.endswith('.py'):
            compile(path.read_text(), str(path), 'exec')
    config = ROOT / 'config.json'
    if config.exists():
        value = json.loads(config.read_text())
        if not isinstance(value, dict) or not isinstance(value.get('DEBUG', False), bool):
            raise ValueError('config.json DEBUG must be a boolean when provided')
    return SOP_NAME


def load_controller():
    name = validate_package()
    sys.path.insert(0, str(ROOT))
    spec = importlib.util.spec_from_file_location("sop_controller", ROOT / "controller.py")
    controller = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(controller)
    controller.SOP_NAME = name
    config = ROOT / 'config.json'
    controller.DEBUG = config.exists() and json.loads(config.read_text()).get('DEBUG') is True
    from sop import Workflow
    if not isinstance(getattr(controller, 'workflow', None), Workflow):
        raise ValueError('Controller must export a Workflow object named workflow')
    return controller


def multica(*args, resource='issue'):
    output = [] if args[:2] == ("comment", "delete") else ["--output", "json"]
    command = ["multica", resource, *args, *output]
    result = subprocess.run(command, capture_output=True, text=True, timeout=30)
    if result.returncode:
        raise SOPError("cli_error", f"{shlex.join(command)}: {result.stderr.strip() or result.stdout.strip()}")
    return json.loads(result.stdout) if result.stdout.strip() else None


def state_key(context):
    key = 'sop.' + context['skill']
    if len(key) > 64:
        raise SOPError('invalid_state_key', 'Issue-bound SOP names must be at most 60 characters')
    return key


def read_state(context, phases=None):
    issue = multica('get', context['issue_id'])
    value = issue['metadata'].get(state_key(context))
    if value is None:
        return {}
    try:
        state = json.loads(value)
    except (TypeError, json.JSONDecodeError) as error:
        raise SOPError('invalid_state', 'Invalid SOP metadata JSON: ' + str(error)) from error
    if (not isinstance(state, dict) or not isinstance(state.get('status'), str) or
            phases is not None and state['status'] not in phases):
        raise SOPError('invalid_state', 'SOP metadata must contain a declared status')
    # Replayed choices use the receiving checkout after an issue handoff.
    for choice in state.get('output', {}).get('next_step', []):
        if choice.get('command'):
            words = shlex.split(choice['command'])
            runtime = state.get('runtime', [])
            if runtime and words[:len(runtime)] == runtime:
                choice['command'] = shlex.join([*runtime_command(), *words[len(runtime):]])
    return state


def save_state(context, state):
    state['runtime'] = runtime_command()
    state['workdir'] = str(Path.cwd())
    try:
        multica('metadata', 'set', context['issue_id'], '--key', state_key(context),
                '--type', 'string', '--value', json.dumps(state, default=str, allow_nan=False, separators=(',', ':')))
    except Exception as error:
        context['state_write_failed'] = True
        raise SOPError('state_write_failed', str(error),
                       'Inspect the saved SOP metadata and reconcile action effects before retrying; do not repeat a completed action.') from error


def delete_state(context):
    clean_questionnaire(context)
    try:
        multica('metadata', 'delete', context['issue_id'], '--key', state_key(context))
    except Exception as error:
        context['state_write_failed'] = True
        raise SOPError('state_write_failed', str(error),
                       'Inspect the saved SOP metadata and reconcile action effects before retrying; do not repeat a completed action.') from error
    context['state_deleted'] = True


def help_output(context):
    context['help'] = True
    state = read_state(context)
    if state:
        context['state'] = state
        output = state.get('output', state)
        render_addressed_state(context, output, refresh=True)
        return json.dumps(redact(agent_decision(output)), allow_nan=False)
    return (ROOT / 'SKILL.md').read_text(encoding='utf-8').split('---', 2)[2].strip()


def assertion_before(workflow, context):
    context['assertion'] = {'boundary': 'before'}
    initial = workflow.before(context)
    context.pop('assertion')
    return initial


def assertion_after(workflow, context, args, initial, result):
    context['assertion'] = {'boundary': 'after', 'initial': initial,
                            'args': vars(args), 'result': result,
                            'path_args': {name: isinstance(value, list) for name, value in vars(args).items()
                                          if isinstance(value, Path) or isinstance(value, list) and value and isinstance(value[0], Path)}}
    workflow.after(context, args, initial, result)
    context.pop('assertion')


def recovery_result(receipt, status, context):
    blocked = status == 'blocked'
    detail = receipt['error']
    if blocked:
        instruction = 'Assertion still fails. Return the evidence to the calling SOP for a fixture and correction.'
        own_name = validate_package()
        target = ROOT.parent / 'multica-surface-block' / 'SKILL.md'
        next_step = callback(instruction)
        if own_name != target.parent.name and target.is_file():
            argv = ['multica', 'sop', 'run', 'multica-surface-block']
            next_step['command'] = shlex.join(argv)
        else:
            next_step['instruction'] += ' Use the calling SOP or parent recovery path; do not recurse into this failed SOP.'
    else:
        repair_args = ['repaired']
        next_step = callback('Correct the unmet assertion, reconcile writes, preserve a fixture, then run repaired once.',
                             repair_args)
    return {'ok': False, 'accepted': False, 'finished': False, 'blocked': blocked,
            'status': status, 'state_changed': False, 'error': detail, 'next_step': next_step,
            'evidence': {'issue_id': context['issue_id'], 'key': state_key(context)},
            'external_write_attempted': receipt.get('external_write_attempted', False),
            'data': receipt.get('assertion', {}).get('result', {}).get('data')}


def validate_result(result, phase, workflow, args):
    next_phase = result['phase']
    accepted = result.get('accepted', True)
    if next_phase not in workflow.states and next_phase != workflow.FINISHED:
        raise ValueError('Controller returned an undeclared phase')
    if not isinstance(accepted, bool):
        raise ValueError('Controller accepted must be a boolean')
    if not accepted and next_phase != phase:
        raise ValueError('Rejected choices must preserve the phase')
    if args.event == 'init' and next_phase != phase and not workflow.is_finished(next_phase):
        raise ValueError('Only an accepted agent callback may advance the phase')


def state_owner(workflow, phase, context):
    if phase == workflow.FINISHED:
        return None
    owner = workflow.states[phase].owner
    owner = owner(context) if callable(owner) else owner
    if owner and 'issue_id' in owner:
        return {key: owner[key] for key in ('issue_id', 'thread', 'id', 'content')}
    return {key: owner[key] for key in ('id', 'name')} if owner else None


def transition(args, context, controller):
    from sop import Completed
    workflow = controller.workflow
    phases = set(workflow.states)
    state = read_state(context, phases | {'error', 'blocked', workflow.FINISHED})
    context['state'] = state
    before = state.get('status')
    context['prior_status'] = before
    phase = before or workflow.initial
    if phase not in {'error', 'blocked'} and workflow.is_finished(phase):
        return {'ok': True, 'status': phase, 'finished': True, 'state_changed': False,
                'next_step': callback(finished_instruction(controller))}
    receipt = None
    if phase in {'error', 'blocked'}:
        receipt = state['recovery']
        if receipt['issue_id'] != context['issue_id']:
            raise SOPError('wrong_recovery', 'Assertion receipt belongs to another issue')
        if phase == 'blocked' or args.event == 'init':
            return recovery_result(receipt, before, context)
        if args.event != 'repaired':
            raise SOPError('invalid_transition', 'Use the returned repaired callback after correcting the assertion')
        context['argv'] = receipt['argv']
        phase = receipt['phase']
        args = arguments(receipt['argv'], controller)

    def cli(*command, write=False):
        record = {'argv': list(command), 'write': write, 'succeeded': False}
        context.setdefault('commands', []).append(record)
        output = multica(*command)
        record['succeeded'] = True
        return output

    if not context['issue_id']:
        raise SOPError('missing_issue_id', 'MULTICA_ISSUE_ID is required')
    context.update(cli=cli, error=SOPError, save_state=lambda: save_state(context, state))
    invalid = None
    try:
        if receipt and receipt['assertion']['boundary'] == 'after':
            saved = receipt['assertion']
            result = saved['result']
            values = dict(saved['args'])
            for name, multiple in saved['path_args'].items():
                values[name] = [Path(value) for value in values[name]] if multiple else Path(values[name])
            assertion_after(workflow, context, SimpleNamespace(**values), saved['initial'], result)
        else:
            context['phase'] = phase
            context['args'] = args
            result = None
            try:
                initial = assertion_before(workflow, context)
            except Completed as error:
                context.pop('assertion', None)
                initial = context.get('initial')
                result = error.result
            if result is None:
                if args.error is not None or args.error_file is not None:
                    message = args.error if args.error is not None else args.error_file.read_text(encoding='utf-8')
                    hint = next((hint for pattern, hint in PATTERNS if re.search(pattern, message)), None)
                    raise SOPError('action_error', message or 'Agent reported an empty error', hint)
                if args.event == 'quit' and phase == workflow.initial and not args.extra:
                    delete_state(context)
                    context['quitted'] = True
                    context['state'] = state
                    message = f'SoP {controller.SOP_NAME} quitted'
                    raise SOPError('quitted', message, message)
                try:
                    if before is None and args.event != 'init':
                        raise SOPError('invalid_transition', 'Start this SOP with a bare call or init before choosing an option')
                    result = workflow.step(args, context, phase)
                except SOPError as error:
                    if error.code not in {'invalid_input', 'invalid_transition'}:
                        raise
                    invalid = {'code': error.code, 'message': str(error)}
                    result = workflow.guidance(workflow.states[phase], context)
                    result['accepted'] = False
            validate_result(result, phase, workflow, args)
            if invalid is None and (args.event != 'init' or result['phase'] != phase):
                assertion_after(workflow, context, args, initial, result)
    except AssertionError as error:
        if 'assertion' not in context:
            raise
        failure = exception_result(error, context)
        status = 'blocked' if receipt else 'error'
        saved = {'issue_id': context['issue_id'], 'phase': phase, 'argv': context['argv'],
                 'assertion': context['assertion'],
                 'error': failure['error'], 'external_write_attempted': failure['external_write_attempted']}
        state.update(status=status, recovery=saved)
        result = recovery_result(saved, status, context)
        result['state_changed'] = True
        return result
    finally:
        context.pop('cli', None)
        context.pop('error', None)
        context.pop('save_state', None)
    if result['phase'] == workflow.initial:
        result.setdefault('alternatives', []).append({'instruction': 'Stop this SOP.', 'callback': ['quit']})
    next_phase = result.pop('phase')
    if next_phase == workflow.FINISHED:
        result['instruction'] = finished_instruction(controller)
    result.setdefault('accepted', True)
    status = next_phase if result['accepted'] or receipt else before
    result['next_step'] = callback(result.pop('instruction'), result.pop('callback', None))
    result['next_step']['option_instruction'] = result.pop('callback_instruction', None)
    alternatives = result.pop('alternatives', [])
    if alternatives:
        result['next_step']['alternatives'] = [callback(choice['instruction'], choice['callback'])
                                                for choice in alternatives]
    owner = state_owner(workflow, next_phase, context)
    if result['accepted'] and owner and 'issue_id' not in owner:
        if workflow.handoff and owner['id'] != context['agent_id'] and not context.get('handoff'):
            workflow.handoff(args, context, owner)
        if before is None or owner != state_owner(workflow, phase, context):
            multica('assign', context['issue_id'], '--to-id', owner['id'], '--no-start')
    finished = workflow.is_finished(next_phase)
    changed = before != (None if finished else status)
    result.update(ok=invalid is None, status=status, finished=finished,
                  state_changed=changed, error=invalid)
    json.dumps(result, allow_nan=False)
    if owner and 'issue_id' in owner and not finished:
        state['owner'] = owner
    else:
        state.pop('owner', None)
    if changed:
        state.pop('recovery', None)
    state['status'] = status
    return result


def redact(value):
    secrets = [v for k, v in os.environ.items() if len(v) >= 4 and re.search(r"(?i)token|password|secret|api.?key|credential", k)]

    def clean(item):
        if isinstance(item, dict):
            return {key: clean(part) for key, part in item.items()}
        if isinstance(item, list):
            return [clean(part) for part in item]
        if not isinstance(item, str):
            return item
        for secret in sorted(secrets, key=len, reverse=True):
            item = item.replace(secret, "[REDACTED]")
        item = re.sub(r"\b(?:gh[pousr]_[A-Za-z0-9_]+|github_pat_[A-Za-z0-9_]+|(?:mat|sk)_[A-Za-z0-9_-]+|sk-[A-Za-z0-9_-]+)", "[REDACTED]", item)
        item = re.sub(r"(?i)(authorization\s*:\s*(?:bearer|basic)\s+)\S+", r"\1[REDACTED]", item)
        return re.sub(r'''(?i)((?:password|token|secret|api[_-]?key)["']?\s*[:=]\s*)(?:\[REDACTED\]|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;}\]]+)''', r"\1[REDACTED]", item)

    return clean(value)


def exception_result(error, context):
    assertion_failure = isinstance(error, AssertionError) and bool(context.get('assertion'))
    hint = error.hint if isinstance(error, SOPError) else None
    if assertion_failure:
        hint = 'Correct the unmet live-state assertion without weakening it; reconcile writes and use the wrapper repair callback once.'
    hint = hint or next((hint for pattern, hint in PATTERNS if re.search(pattern, str(error))), None)
    code = "assertion_failed" if assertion_failure else error.code if isinstance(error, SOPError) else "controller_failure"
    detail = {"code": code, "type": type(error).__name__, "message": str(error),
              "hint": hint or "Create a reproducing regression fixture; repair the controller under its rules, then retry this call once. Stop if the repair still fails."}
    if hint is None or not isinstance(error, SOPError):
        detail["rules"] = RULES
        detail["workflow_rules"] = context.get("controller_rules", "Read controller.py; preserve the workflow contract.")
        detail["fixture_request"] = "Preserve the failing argv, relevant Multica IDs, and prior SOP metadata; add a failing local fixture before changing the script."
    writes = [command for command in context.get("commands", []) if command["write"]]
    write_attempted = bool(writes) or context.get('external_write_attempted', False)
    retry = context["argv"] if not isinstance(error, SOPError) and not write_attempted else []
    recovery = callback(detail["hint"], retry)
    if write_attempted:
        recovery["instruction"] += " A Multica write was attempted: inspect its live result before repeating it; resume to reconcile."
    result = {"ok": False, "status": context.get("prior_status"), "accepted": False, "finished": False,
              "state_changed": False, "error": redact(detail),
              "next_step": recovery, "external_write_attempted": write_attempted}
    return result


def agent_decision(output):
    return {key: output[key] for key in ('status', 'instruction', 'next_step', 'error', 'evidence', 'usage') if key in output}


# TODO: finish state pending definition. Distinguish re-entry from end-loop
# completion; delegation cleans SOP state at the ringed-circle for re-entry.
# Blocked runs do not reach that node. No end-loop mode is defined yet.
def finished_instruction(controller):
    return (controller.SOP_NAME + ' SoP finished, directly and silently end this turn. '
            'Allow re-enter with initial command in SKILL.md next round.')


def response(result, *, reply_guidance=True):
    """Agent guidance for the current issue state."""
    if reply_guidance and 'this_round_reply_guidance' in result:
        return {'status': result['status'], 'instruction': result['this_round_reply_guidance']}
    step = result['next_step']
    instruction = step['instruction']
    if result['status'] == 'FINISHED':
        return {'status': result['status'], 'instruction': instruction}
    if result['status'] is not None:
        instruction = f'You are in SOP state "{result["status"]}". {instruction}'
    options = []
    if step.get('command'):
        options.append({'instruction': step.get('option_instruction') or step['instruction'],
                        'command': step['command']})
    options.extend(step.get('alternatives', []))
    output = {'status': result['status'], 'instruction': instruction, 'next_step': options}
    if result.get('error'):
        output['error'] = {key: result['error'][key] for key in ('code', 'message')}
    for key in ('evidence', 'usage'):
        value = result.get(key)
        if isinstance(value, dict):
            value = {k: v for k, v in value.items() if v not in (None, '', [], {})}
        if value not in (None, '', [], {}):
            output[key] = value
    return agent_decision(output)


def graph_svg(source, identifier):
    dot = ensure_command('dot', 'graphviz')
    result = subprocess.run([dot, '-Tsvg', '-Gid=' + identifier,
                             '-Nfontname=Helvetica', '-Nstyle=filled', '-Nmargin=0.2',
                             '-Nfillcolor=#eef2ff', '-Ncolor=#94a3b8',
                             '-Ecolor=#64748b'], input=source, text=True,
                            capture_output=True, timeout=30)
    if result.returncode:
        raise ValueError('Invalid DOT diagram: ' + result.stderr.strip())
    return result.stdout[result.stdout.index('<svg'):]


def compile_preview(controller):
    """Compile declarations only, without evaluating runtime hooks or factories."""
    workflow = controller.workflow
    nodes = {}

    def text(value, location):
        if not isinstance(value, str):
            raise ValueError(location + ' needs an explicit static preview string')
        return value

    def parameters(params):
        return [{'flag': param.flag, 'prompt': param.prompt, 'required': param.required,
                 'value': param.value, **{key: value for key, value in param.kwargs.items()
                                         if key in ('choices', 'default', 'nargs', 'action')}}
                for param in params]

    declared, edges = workflow.graph()
    for key, node in declared.items():
        instruction = node.prompt
        if isinstance(node, sop.State) and (node.preview is not None or node.view):
            instruction = node.preview
        value = {'name': node.name, 'instruction': text(instruction, type(node).__name__ + ' ' + node.name)}
        nodes[key] = value
        destinations = [target for source, target in edges if source == key]
        if key == workflow.FINISHED:
            value.update(kind='finished', instruction=finished_instruction(controller))
        elif isinstance(node, sop.State):
            value.update(kind='state', next_step=[
                {**callback(declared[target].prompt, declared[target].callback()), 'node': target}
                for target in destinations])
        else:
            value.update(kind='action' if isinstance(node, sop.Option) else 'branch', destinations=destinations)
            if isinstance(node, sop.Option):
                value['parameters'] = parameters(node.params)
                if node.this_round_reply_guidance_preview is not None:
                    value['this_round_reply_guidance'] = text(node.this_round_reply_guidance_preview, 'Option ' + node.name)
    return {'initial': workflow.initial, 'parameters': parameters(workflow.params),
            'nodes': nodes, 'edges': [list(edge) for edge in edges]}


def state_artifact(context, controller, result):
    """Render the static declaration JSON beside the resolved current-state output."""
    def owner_label(node):
        try:
            owner = node.owner(context) if callable(node.owner) else node.owner
        except KeyError:
            return '{owner}'
        if not owner:
            return ''
        if 'issue_id' not in owner:
            return owner['name']
        if not owner.get('id'):
            return 'outer:human'
        name = owner.get('name') or (owner['id'] if '{' in owner['id'] else
                                     cli('agent', 'get', owner['id'])['name'])
        return 'outer:' + name

    preview = compile_preview(controller)
    status = result['status']
    recovery = context.get('state', {}).get('recovery', {})
    assertion = recovery.get('assertion', {})
    phase = status
    if result.get('error'):
        phase = (assertion.get('result', {}).get('phase') or recovery.get('phase') or
                 context.get('phase') or context.get('prior_status') or preview['initial'])
    details = {'issue_id': context['issue_id'], 'sop': controller.SOP_NAME, 'status': status}
    if result.get('error'):
        details.update(failed_state=phase, boundary=assertion.get('boundary'), error=result['error'])
    current = agent_decision(context.get('output', {}))
    quote = json.dumps
    diagram = 'digraph workflow {\nrankdir=LR; bgcolor="transparent";\nnode [shape=box];\n'
    for key, value in preview['nodes'].items():
        label = escape(value['name'])
        if value['kind'] == 'state':
            owner = owner_label(controller.workflow.states[key])
            if owner:
                label += '<BR/><FONT POINT-SIZE="10">' + escape(owner) + '</FONT>'
        attributes = ['id=' + quote('state_' + key), 'label=<' + label + '>']
        if value['kind'] == 'action':
            attributes += ['shape=plain', 'style=""', 'fontsize=12']
        elif value['kind'] == 'branch':
            attributes += ['shape=diamond']
        elif value['kind'] == 'finished':
            attributes += ['shape=doublecircle', 'label=""', 'fillcolor=black', 'color=black',
                           'width=0.2', 'height=0.2', 'fixedsize=true']
        if key == phase:
            attributes += ['color="#2563eb"', 'penwidth=2']
            if value['kind'] != 'action':
                attributes += ['fillcolor=' + quote('#fee2e2' if result.get('error') else '#dcfce7')]
        diagram += quote(key) + ' [' + ', '.join(attributes) + '];\n'
    diagram += '__start [shape=point, label="", fillcolor=black, color=black, width=0.05, height=0.05];\n'
    diagram += '__start -> ' + quote(preview['initial']) + ';\n'
    diagram += '\n'.join(quote(source) + ' -> ' + quote(target) + (' [arrowhead=none]' if preview['nodes'][target]['kind'] == 'action' else '') + ';'
                         for source, target in preview['edges']) + '\n}'
    data = {'template': preview, 'current': current, 'actions': context.get('action_outputs', {})}
    payload = json.dumps(redact(data), separators=(',', ':')).replace('<', '\\u003c')
    return ('<!doctype html><html lang="en"><meta charset="utf-8">'
            '<meta name="viewport" content="width=device-width, initial-scale=1"><title>' + escape(controller.SOP_NAME) + '</title>'
            '<style>body{font:15px system-ui;margin:8px}pre{white-space:pre-wrap;overflow-wrap:anywhere;overflow-x:auto}'
            '.diagram{margin:0;max-width:100%;overflow-x:auto}.diagram svg{display:block;max-width:none;margin:auto}'
            '@media(orientation:landscape){.diagram{overflow-x:hidden}.diagram svg{max-width:100%;height:auto}}'
            '[role="button"]{cursor:pointer}h1{font-size:18px}</style>'
            '<div class="diagram" id="workflow">' + graph_svg(diagram, 'workflow_graph') + '</div>'
            '<h1>' + escape(controller.SOP_NAME) + '</h1><h2 id="current-heading" hidden>Resolved current state</h2><pre id="current" hidden>'
            + escape(json.dumps(redact(current), indent=2)) + '</pre>'
            '<h2>Static template</h2><pre id="detail" aria-live="polite"></pre>'
            '<h2 id="action-heading" hidden>Resolved action output</h2><pre id="action" hidden></pre>'
            '<pre id="metadata">' + escape(json.dumps(redact(details), indent=2)) + '</pre>'
            '<script id="sop-data" type="application/json">' + payload + '</script>'
            '<script>'
            'const data=JSON.parse(document.getElementById("sop-data").textContent),detail=document.getElementById("detail"),action=document.getElementById("action"),current=document.getElementById("current"),currentHeading=document.getElementById("current-heading");'
            'const show=(name,clicked=true)=>{current.hidden=currentHeading.hidden=!clicked||name!==' + quote(phase) + ';detail.textContent=JSON.stringify(data.template.nodes[name],null,2);'
            'action.hidden=!data.actions[name];document.getElementById("action-heading").hidden=action.hidden;'
            'action.textContent=data.actions[name]?JSON.stringify(data.actions[name],null,2):""};'
            'for(const name of Object.keys(data.template.nodes)){'
            'const node=document.getElementById(`state_${name}`);if(!node)continue;'
            'node.setAttribute("tabindex","0");node.setAttribute("role","button");'
            'node.onclick=()=>show(name);node.onkeydown=event=>{if(event.key==="Enter"||event.key===" "){'
            'event.preventDefault();show(name)}}}'
            'show(' + quote(phase if phase in preview['nodes'] else preview['initial']) + ',false);'
            '</script></html>')


def reply_parent(context):
    # TODO: SoP as first class member Stage 4: temporary comment-transport hotfix.
    # https://github.com/Winfred666/multica-RSI/issues/77
    if context['issue_id'] == context.get('native_issue_id') and context.get('native_comment_id'):
        return context['native_comment_id']
    return getattr(context.get('args'), 'thread', None) or context.get('state', {}).get('reply_parent')


def publish_artifacts(context, controller, result):
    if not controller:
        return
    shifted = result.get('state_changed') or result.get('finished')
    if not (shifted or result.get('error') or
            result.get('accepted') and context['argv'] and context['argv'][0] != 'init'):
        return
    try:
        config = ROOT / 'config.json'
        if config.exists():
            publish_shared_artifact({'issue_id': context['issue_id'], 'filename': controller.SOP_NAME + '-config.json'},
                                    config.read_text())
        artifacts = {}
        # In DEBUG mode, update the controller workflow's state chart artifact on every state shift.
        if controller.DEBUG and shifted:
            publish_shared_artifact({'issue_id': context['issue_id'], 'filename': controller.SOP_NAME + '-state.html'},
                                    state_artifact(context, controller, result))
        if not result.get('error') and hasattr(controller, 'artifacts'):
            artifacts.update(controller.artifacts({**context, 'cli': multica, 'error': SOPError}))
        parent = reply_parent(context)
        for filename, content in artifacts.items():
            write_artifact({'issue_id': context['issue_id'], 'filename': filename}, content, parent)
    except Exception as error:
        print(redact('Live artifact update failed: ' + str(error)), file=sys.stderr)


def clean_questionnaire(context):
    state = context['state']
    publication = state.get('questionnaire', {})
    for key in ('forwarded_comment_id', 'que_comment_id'):
        if publication.get(key):
            context['external_write_attempted'] = True
            multica('comment', 'delete', publication[key])
            publication.pop(key)
            save_state(context, state)
    state.pop('questionnaire', None)


def render_addressed_state(context, rendered, *, refresh=False):
    """Retry publication from saved state without replaying the entering action."""
    state = context['state']
    publication = state.get('questionnaire', {})
    if publication and (refresh or publication['status'] != state['status']):
        clean_questionnaire(context)
        save_state(context, state)
    owner = state.get('owner')
    if not owner:
        return
    publication = state.setdefault('questionnaire', {'status': state['status']})
    destination = {key: owner[key] for key in ('issue_id', 'thread', 'id')}
    answer = '!sop-remote ' + context['skill'] + ' {"args":[...]}'
    command = shlex.join(['multica', 'issue', 'comment', 'add', context['issue_id'],
                          '--content-file', '<answer-file>'])
    rendered = {**rendered, 'next_step': [
        {**step, 'command': step['command'] + ' --issue ' + shlex.quote(context['issue_id'])}
        for step in rendered.get('next_step', [])], 'how_to_step': (
        'Save ' + answer + ' in a UTF-8 file, then post it with: ' + command + '. '
        'Replace [...] with the chosen next_step command arguments after multica sop run ' + context['skill'] +
        ', including --issue ' + context['issue_id'] +
        ' and filling its placeholders; args must be a JSON array of strings.')}
    if not publication.get('que_comment_id'):
        prefix = '' if owner['issue_id'] != context['issue_id'] else owner['content'].removeprefix('/note').strip()
        header = (prefix + ' !sop-render ' + context['skill'] + ' ' + json.dumps(destination)).lstrip()
        context['external_write_attempted'] = True
        publication['que_comment_id'] = comment(context['issue_id'],
            header + '\n' + json.dumps(rendered, allow_nan=False), reply_parent(context))['id']
        save_state(context, state)
    if owner['issue_id'] != context['issue_id'] and not publication.get('forwarded_comment_id'):
        app_url = os.environ.get('MULTICA_APP_URL', '').rstrip('/')
        if not app_url.startswith(('http://', 'https://')):
            raise SOPError('missing_app_url', 'Set app_url to the frontend HTTP(S) URL before forwarding questionnaires')
        workspace = request('/api/workspaces/' + context['workspace_id'])
        link = app_url + '/' + workspace['slug'] + '/issues/' + context['issue_id'] + '#comment-' + publication['que_comment_id']
        context['external_write_attempted'] = True
        publication['forwarded_comment_id'] = comment(owner['issue_id'],
            owner['content'] + '\n\n[Question](' + link + ')', owner['thread'])['id']
        save_state(context, state)


def main(argv=None):
    argv = list(sys.argv[1:] if argv is None else argv)
    if argv == ['--validate']:
        try:
            validate_package()
            load_controller().workflow.validate_graph(reply_guidance=True)
            print(json.dumps({'status': 'valid'}))
            return 0
        except Exception as error:
            print(json.dumps({'status': 'error', 'error': str(error)}))
            return 1
    context = {"agent_id": os.environ.get("MULTICA_AGENT_ID"),
               "issue_id": os.environ.get("MULTICA_ISSUE_ID"),
               "native_issue_id": os.environ.get("MULTICA_ISSUE_ID"),
               "native_comment_id": os.environ.get("MULTICA_COMMENT_ID"),
               "workspace_id": os.environ.get("MULTICA_WORKSPACE_ID"),
               "argv": argv, "sop_dir": ROOT, "state_dir": STATE_DIR}
    controller = None
    try:
        for key in ('issue_id', 'workspace_id'):
            if not context[key]:
                raise SOPError('missing_' + key, 'MULTICA_' + key.upper() + ' is required')
        if argv == ['--help']:
            context['skill'] = validate_package()
            print(help_output(context))
            return 0
        controller = load_controller()
        context['skill'] = controller.SOP_NAME
        args = arguments(context['argv'], controller)
        if getattr(args, 'workspace', None) and args.workspace != context['workspace_id']:
            raise SOPError('wrong_workspace', '--workspace must match the native run workspace')
        for flag, key in (('issue', 'issue_id'), ('workspace', 'workspace_id')):
            if getattr(args, flag, None):
                context[key] = getattr(args, flag)
        os.environ['MULTICA_ISSUE_ID'] = context['issue_id']
        context['controller_rules'] = 'Follow controller.workflow options and state prechecks.'
        if args.help:
            print(help_output(context))
            return 0
        result = transition(args, context, controller)
    except Exception as error:
        result = exception_result(error, context)
        if context.get('quitted'):
            result.update(status='quitted', state_changed=True, next_step=callback(str(error)))
    output = redact(response(result))
    rendered = redact(response(result, reply_guidance=False))
    try:
        if controller and context.get('state') and not context.get('help'):
            state = context['state']
            state['output'] = rendered
            if not context.get('state_deleted') and not context.get('state_write_failed'):
                save_state(context, state)
                if result['ok']:
                    if result.get('finished'):
                        delete_state(context)
                    else:
                        render_addressed_state(context, rendered)
    except Exception as error:
        result = exception_result(error, context)
        output = redact(response(result))
    context['output'] = rendered
    publish_artifacts(context, controller, result)
    print(redact(context['handoff']) if result['ok'] and context.get('handoff') and
          'this_round_reply_guidance' not in result else json.dumps(output, allow_nan=False))
    return 0 if result["ok"] and result.get("accepted", True) else result.get("returncode", 1)


if __name__ == "__main__":
    sys.modules['wrapper'] = sys.modules[__name__]
    parser = argparse.ArgumentParser()
    parser.add_argument('--sop-dir', type=Path, required=True)
    parser.add_argument('--state-dir', type=Path, required=True)
    parser.add_argument('--name', required=True)
    boundary = sys.argv.index('--')
    engine = parser.parse_args(sys.argv[1:boundary])
    ROOT, STATE_DIR, SOP_NAME = engine.sop_dir.resolve(), engine.state_dir.resolve(), engine.name
    sys.exit(main(sys.argv[boundary + 1:]))
