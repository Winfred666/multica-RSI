#!/usr/bin/env python3
"""Run explicit workspace operations with a host-provisioned member descriptor."""
import argparse
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from uuid import UUID


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--target', help='Target workspace UUID, except for create')
    parser.add_argument('operation', choices=('create', 'import', 'inspect', 'export', 'run'))
    parser.add_argument('args', nargs=argparse.REMAINDER, help='Native multica arguments; use absolute file paths')
    args = parser.parse_args()
    words = args.args[1:] if args.args[:1] == ['--'] else args.args
    if args.operation == 'create':
        if args.target:
            parser.error('create returns a new workspace ID; do not pass --target')
        target = ''
    else:
        try:
            target = str(UUID(args.target))
        except (ValueError, TypeError, AttributeError):
            parser.error('--target must be a workspace UUID')
    if any(w.split('=')[0] in {'--server-url', '--workspace-id', '--profile', '--debug'} for w in words):
        parser.error('server, workspace, profile and debug overrides are not allowed')
    if args.operation == 'run':
        if not words or words[0] not in {
            'project', 'issue', 'agent', 'runtime', 'repo', 'skill', 'label',
            'property', 'squad', 'view', 'workflow', 'autopilot', 'attachment',
        }:
            parser.error('run requires a workspace resource command, not a local/auth command')
    else:
        operation = 'import' if args.operation == 'inspect' else args.operation
        words = ['workspace', operation, *([target] if target else []), *words]
        if args.operation == 'inspect':
            words.append('--inspect')
    try:
        fd = int(os.environ['MULTICA_WORKSPACE_MEMBER_FD'])
        if fd < 3:
            raise ValueError()
        # Seekable host descriptors can be reused across commands in one authorized session.
        try:
            os.lseek(fd, 0, os.SEEK_SET)
        except OSError:
            pass
        token = os.read(fd, 257).decode().strip()
    except (KeyError, ValueError, OSError, UnicodeError):
        parser.error('member_context_required: host must supply a member PAT through MULTICA_WORKSPACE_MEMBER_FD')
    if not token.startswith(('mul_', 'mcn_')) or len(token) > 256 or any(c.isspace() for c in token):
        parser.error('member_context_required: the descriptor must contain a member PAT, not a task token')
    server = os.environ.get('MULTICA_SERVER_URL')
    if not server:
        parser.error('MULTICA_SERVER_URL is required')
    # Shared /tmp may have a task marker; never remove or bypass that marker.
    with tempfile.TemporaryDirectory(prefix='multica-member-', dir=Path.home()) as work:
        env = {'PATH': os.environ.get('PATH', ''), 'HOME': work,
               'MULTICA_TOKEN': token, 'MULTICA_SERVER_URL': server,
               'MULTICA_WORKSPACE_ID': target}
        try:
            result = subprocess.run([os.environ.get('MULTICA_CLI_PATH', 'multica'), *words, '--output', 'json'],
                                    cwd=work, env=env, capture_output=True, text=True, timeout=120)
        except (OSError, subprocess.TimeoutExpired):
            sys.exit('Member CLI failed or timed out. Reconcile target state before retrying.')
    for output, stream in ((result.stdout, sys.stdout), (result.stderr, sys.stderr)):
        output = re.sub(r'\b(?:mul|mcn|mat)_[A-Za-z0-9_-]+', '[REDACTED]', output.replace(token, '[REDACTED]'))
        stream.write(output)
    return result.returncode


if __name__ == '__main__':
    sys.exit(main())
