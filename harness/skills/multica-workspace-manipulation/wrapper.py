#!/usr/bin/env python3
"""Run explicit workspace operations with the host member CLI profile."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
from uuid import UUID

from auth import load
from inbox import wait


def redact(output, token):
    return re.sub(r'\b(?:mul|mcn|mat)_[A-Za-z0-9_-]+', '[REDACTED]', output.replace(token, '[REDACTED]'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--member-config', help='Authorized member CLI config; defaults to ~/.multica/config.json')
    parser.add_argument('--target', help='Target workspace UUID, except for create')
    parser.add_argument('operation', choices=('create', 'import', 'inspect', 'export', 'run', 'inbox-wait'))
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
    if args.operation == 'inbox-wait':
        if words:
            parser.error('inbox-wait takes no extra arguments')
    elif args.operation == 'run':
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
        server, token = load(args.member_config)
    except (OSError, ValueError, KeyError, AttributeError, TypeError):
        parser.error('member_context_required: authorize the host member CLI profile or supply --member-config / an inherited descriptor')
    if args.operation == 'inbox-wait':
        try:
            event = wait(server, token, target)
        except KeyboardInterrupt:
            return 130
        except (OSError, ValueError, KeyError, TypeError):
            sys.exit('Inbox request failed. Reconcile inbox read state before retrying.')
        print(redact(json.dumps(event, ensure_ascii=False, indent=2), token))
        return 0
    cli = shutil.which(os.environ.get('MULTICA_CLI_PATH', 'multica'))
    if not cli:
        parser.error('multica CLI not found on this runtime host')
    # Shared /tmp may have a task marker; never remove or bypass that marker.
    with tempfile.TemporaryDirectory(prefix='multica-member-', dir=Path.home()) as work:
        env = {k: os.environ[k] for k in ('PATH', 'SystemRoot', 'SYSTEMROOT', 'WINDIR', 'PATHEXT') if k in os.environ}
        env.update(HOME=work, USERPROFILE=work, MULTICA_TOKEN=token,
                   MULTICA_SERVER_URL=server, MULTICA_WORKSPACE_ID=target)
        try:
            result = subprocess.run([cli, *words, '--output', 'json'],
                                    cwd=work, env=env, capture_output=True, text=True, timeout=120)
        except (OSError, subprocess.TimeoutExpired):
            sys.exit('Member CLI failed or timed out. Reconcile target state before retrying.')
    for output, stream in ((result.stdout, sys.stdout), (result.stderr, sys.stderr)):
        stream.write(redact(output, token))
    return result.returncode


if __name__ == '__main__':
    sys.exit(main())
