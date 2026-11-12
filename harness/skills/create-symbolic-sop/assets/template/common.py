"""Ordinary comment artifact publication shared by the SoP runtime."""
import json
from pathlib import Path
import subprocess
import tempfile


def write_artifact(reference, content, parent=None, message=' '):
    # TODO: Replace immutable attachment revisions with native deliverable update.
    # The stable owner/filename reference resolves the newest revision, across issues.
    if not parent:
        roots = cli('issue', 'comment', 'list', reference['issue_id'], '--roots-only', '--summary')
        parent = roots[-1]['id'] if roots else None
    with tempfile.TemporaryDirectory(prefix='.featssship-', dir=Path.cwd()) as directory:
        path = Path(directory) / reference['filename']
        path.write_text(content)
        # Grilling supplies a confirmation prompt; other revisions render empty.
        words = ['issue', 'comment', 'add', reference['issue_id'], '--content', message, '--attachment', str(path)]
        if parent:
            words += ['--parent', parent]
        return cli(*words)


def run(*words, cwd=None, input=None):
    result = subprocess.run(words, cwd=cwd, input=input, text=True, capture_output=True)
    if result.returncode:
        raise ValueError(result.stderr.strip() or result.stdout.strip() or str(words))
    return result.stdout.strip()


def cli(*words, input=None):
    return json.loads(run('multica', *words, '--output', 'json', input=input))
