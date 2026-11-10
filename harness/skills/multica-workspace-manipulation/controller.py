"""Explicit host-authorized member operations, separate from native task state."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
from uuid import UUID

from sop import Option, Parameter, State, Workflow


def member(context, words, target='', *, write=False):
    """Use only a separately provisioned member credential, never the task/profile."""
    try:
        fd = int(os.environ['MULTICA_WORKSPACE_MEMBER_FD'])
        if fd < 3:
            raise ValueError()
        token = os.read(fd, 4096).decode().strip()
    except (KeyError, ValueError, OSError, UnicodeError):
        raise context['error']('member_context_required',
            'Host owner must launch this invocation with a member PAT on MULTICA_WORKSPACE_MEMBER_FD. '
            'Task credentials and remote-sop alone cannot authorize this operation.',
            'Request a private host credential handoff, then rerun the explicit operation with a fresh descriptor.') from None
    if len(token) > 256 or not token.startswith(('mul_', 'mcn_')) or any(c.isspace() for c in token):
        raise context['error']('member_context_required', 'The host descriptor must contain a member PAT, not a task credential.')
    cli = os.environ.get('MULTICA_CLI_PATH', 'multica')
    # A shared temporary root can carry another task's persistent CLI marker.
    with tempfile.TemporaryDirectory(prefix='multica-member-', dir=Path.home()) as work:
        env = {'PATH': os.environ.get('PATH', ''), 'HOME': work,
               'MULTICA_TASK_CONFIG_ROOT': work, 'MULTICA_TOKEN': token,
               'MULTICA_SERVER_URL': os.environ['MULTICA_SERVER_URL'],
               'MULTICA_WORKSPACE_ID': target}
        context['external_write_attempted'] = write
        try:
            result = subprocess.run([cli, *words, '--output', 'json'], cwd=work,
                                    env=env, capture_output=True, text=True, timeout=120)
        except (OSError, subprocess.TimeoutExpired):
            raise context['error']('member_execution_failed',
                'Member CLI failed or timed out. Reconcile target state before retrying.') from None
    def clean(text):
        return re.sub(r'\b(?:mul|mcn|mat)_[A-Za-z0-9_-]+', '[REDACTED]', text.replace(token, '[REDACTED]'))
    output = clean(result.stdout)
    if result.returncode:
        raise context['error']('member_command_failed', clean(result.stderr or result.stdout)[:2000])
    return json.loads(output) if output.strip() else {}


def remember(context, target):
    context['state']['target'] = str(UUID(target))
    context['save_state']()


def create(args, context):
    result = member(context, ['workspace', 'create', '--name', args.name, '--slug', args.slug], write=True)
    remember(context, result['id'])
    return {'instruction': 'Created target workspace ' + context['state']['target'] + '. Select an explicit operation.'}


def select(args, context):
    target = str(UUID(args.target))
    result = member(context, ['workspace', 'get', target], target)
    remember(context, result['id'])


def package(args, context):
    target = context['state']['target']
    path = str(Path(args.file).expanduser().resolve())
    if args.event == 'export':
        if Path(path).exists():
            raise context['error']('output_exists', 'Choose a new private export path; existing files are preserved.')
        words = ['workspace', 'export', target, '--file', path,
                 '--selection-file', str(Path(args.selection_file).expanduser().resolve())]
    else:
        words = ['workspace', 'import', target, '--file', path]
        if args.event == 'inspect':
            words.append('--inspect')
        if args.selection_file:
            words += ['--selection-file', str(Path(args.selection_file).expanduser().resolve())]
    result = member(context, words, target, write=args.event != 'inspect')
    return {'instruction': f'{args.event} in {target}: ' + json.dumps(result)[:3000]}


def run(args, context):
    words = json.loads(args.argv_json)
    if (not isinstance(words, list) or len(words) < 2 or
            not all(isinstance(word, str) for word in words) or
            words[0] not in {'project', 'issue', 'agent', 'runtime'}):
        raise context['error']('invalid_command', 'Supply a JSON argv array beginning with project, issue, agent or runtime. No shell.')
    if any(word.split('=')[0] in {'--server-url', '--workspace-id', '--profile', '--debug'} for word in words):
        raise context['error']('context_override', 'Server, workspace, profile and debug overrides are not allowed.')
    result = member(context, words, context['state']['target'], write=True)
    return {'instruction': 'Target ' + context['state']['target'] + ': ' + json.dumps(result)[:3000]}


def view(context):
    return {'instruction': 'Selected target: ' + context['state'].get('target', '(none)') +
            '. Each operation requires a fresh host-provisioned member descriptor. Reconcile writes before retrying.'}


file_param = Parameter('--file', 'Absolute private package path')
selection_param = Parameter('--selection-file', 'Native selection JSON path', required=False)
workflow = Workflow(
    State('choose', 'Create a workspace or select an existing target by UUID.', initial=True, options=(
        Option('create', 'operate', 'Create an empty workspace as the authorized member.', create, params=(
            Parameter('--name', 'Workspace name'), Parameter('--slug', 'Permanent workspace slug'))),
        Option('select', 'operate', 'Verify member access to an existing target.', select, params=(
            Parameter('--target', 'Target workspace UUID'),)),
        Option('finish', 'finished', 'Stop without further writes'),
    )),
    State('operate', 'Operate explicitly in the selected workspace.', view=view, options=(
        Option('inspect', 'operate', 'Inspect a package without importing.', package, params=(file_param, selection_param)),
        Option('import', 'operate', 'Import a native package; reconcile before retrying.', package, params=(file_param, selection_param)),
        Option('export', 'operate', 'Export only selected resources to a new private file.', package, params=(
            file_param, Parameter('--selection-file', 'Native export selection JSON path'))),
        Option('run', 'operate', 'Run the member CLI in the selected workspace.', run, params=(
            Parameter('--argv-json', 'JSON array of member CLI arguments, without multica'),)),
        Option('change', 'choose', 'Choose another target'),
        Option('finish', 'finished', 'Finish and clear only this SOP state'),
    )),
    State('finished', 'Workspace operations finished. Deliver native IDs and task-dispatch evidence.', final=True),
)
