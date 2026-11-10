#!/usr/bin/env python3
"""Host launcher: read an explicitly authorized member profile, then pass a private FD."""
import json
import os
from pathlib import Path
import subprocess
import sys


def main():
    if len(sys.argv) < 3:
        sys.exit('Usage: auth.py /authorized/member-config.json [--target UUID] create|import|inspect|export|run ...')
    try:
        profile = json.loads(Path(sys.argv[1]).expanduser().read_text())
        token = profile['token'].strip()
        server = profile['server_url']
        if not token.startswith(('mul_', 'mcn_')) or len(token) > 256 or not server:
            raise ValueError()
    except (OSError, ValueError, KeyError, AttributeError, TypeError):
        sys.exit('The explicitly selected host profile must contain a member PAT and server_url.')
    read, write = os.pipe()
    try:
        os.write(write, token.encode())
        os.close(write)
        env = {**os.environ, 'MULTICA_WORKSPACE_MEMBER_FD': str(read), 'MULTICA_SERVER_URL': server}
        return subprocess.run([sys.executable, str(Path(__file__).with_name('wrapper.py')), *sys.argv[2:]],
                              env=env, pass_fds=(read,)).returncode
    finally:
        os.close(read)


if __name__ == '__main__':
    sys.exit(main())
