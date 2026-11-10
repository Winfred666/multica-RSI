"""Member authentication shared by the script entry point on every runtime host."""
import json
import os
from pathlib import Path


def load(config=None):
    if config is None and 'MULTICA_WORKSPACE_MEMBER_FD' in os.environ:
        fd = int(os.environ['MULTICA_WORKSPACE_MEMBER_FD'])
        if fd < 3:
            raise ValueError('Member descriptor must be 3 or higher.')
        try:
            os.lseek(fd, 0, os.SEEK_SET)
        except OSError:
            pass
        token = os.read(fd, 257).decode().strip()
        server = os.environ.get('MULTICA_SERVER_URL')
    else:
        path = Path(config).expanduser() if config else Path.home() / '.multica' / 'config.json'
        profile = json.loads(path.read_text(encoding='utf-8'))
        token, server = profile['token'].strip(), profile['server_url']
    if (not token.startswith(('mul_', 'mcn_')) or len(token) > 256 or
            any(c.isspace() for c in token) or not isinstance(server, str) or not server):
        raise ValueError('A member PAT and server URL are required.')
    return server, token
