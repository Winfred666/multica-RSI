"""Shared configuration artifacts and ordinary comment publication, from R-328."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
from urllib.request import Request, urlopen
import uuid

CONFIG_KEY = 'featssship.config'
DELEGATION_KEY = 'featssship.delegation'


def run(*words, cwd=None, input=None):
    result = subprocess.run(words, cwd=cwd, input=input, text=True, capture_output=True)
    if result.returncode:
        raise ValueError(result.stderr.strip() or result.stdout.strip() or str(words))
    return result.stdout.strip()


def cli(*words, input=None):
    output = () if words[0] == 'attachment' else ('--output', 'json')
    return json.loads(run('multica', *words, *output, input=input))


def config_ref(issue, key=CONFIG_KEY):
    value = issue.get('metadata', {}).get(key)
    return json.loads(value) if isinstance(value, str) else value


def link_config(issue_id, reference, key=CONFIG_KEY):
    cli('issue', 'metadata', 'set', issue_id, '--key', key,
        '--value', json.dumps(reference), '--type', 'string')


def request(path, data=None, content_type=None, *, with_headers=False):
    headers = {'Authorization': 'Bearer ' + os.environ['MULTICA_TOKEN'],
               'X-Workspace-ID': os.environ['MULTICA_WORKSPACE_ID']}
    if content_type:
        headers['Content-Type'] = content_type
    with urlopen(Request(os.environ['MULTICA_SERVER_URL'].rstrip('/') + path,
                         data=data, headers=headers), timeout=30) as response:
        body = json.load(response)
        return (body, response.headers) if with_headers else body


def comment(issue_id, content, parent=None, attachment_ids=()):
    body = {'content': content}
    if parent:
        body['parent_id'] = parent
    if attachment_ids:
        body['attachment_ids'] = list(attachment_ids)
    return request('/api/issues/' + issue_id + '/comments',
                   json.dumps(body, allow_nan=False).encode(), 'application/json')


def attachment(reference):
    if reference.get('attachment_id'):
        return request('/api/attachments/' + reference['attachment_id'])
    # Existing R-328 shared references resolve the latest owner/filename revision.
    owner = cli('issue', 'get', reference['issue_id'])
    rows = [row for row in owner.get('attachments', []) if row['filename'] == reference['filename']]
    if not rows:
        raise ValueError('Configuration attachment is missing: ' + json.dumps(reference))
    row = max(rows, key=lambda row: (row.get('created_at', ''), row['id']))
    reference['attachment_id'] = row['id']
    return row


def download_config(reference, directory):
    row = attachment(reference)
    downloaded = json.loads(run('multica', 'attachment', 'download', row['id'], '--output-dir', str(directory)))
    return Path(downloaded['path'])


def read_config(reference):
    with tempfile.TemporaryDirectory(prefix='.sop-', dir=Path.cwd()) as directory:
        return json.loads(download_config(reference, directory).read_text())


def write_config(reference, config, parent=None, message=' '):
    return _write_resource(reference, json.dumps(config, indent=2, allow_nan=False) + '\n',
                           'application/json', parent, message)


def _write_resource(reference, content, content_type, parent, message, *, independent=True):
    """Upload a file and publish its link or comment attachment."""
    fields = {} if independent else {'issue_id': reference['issue_id']}
    if independent and reference.get('attachment_id'):
        fields['attachment_id'] = reference['attachment_id']
    boundary = 'sop-' + uuid.uuid4().hex
    body = b''.join((f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n').encode()
                    for key, value in fields.items())
    body += (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{reference["filename"]}"\r\n'
             f'Content-Type: {content_type}\r\n\r\n').encode()
    body += content.encode() + f'\r\n--{boundary}--\r\n'.encode()
    row = request('/api/upload-file', body, 'multipart/form-data; boundary=' + boundary)
    if independent:
        reference['attachment_id'] = row['id']
    # Config callers persist this reference before other issues resume reading it.
    try:
        if not parent:
            roots = cli('issue', 'comment', 'list', reference['issue_id'], '--roots-only', '--summary')
            parent = roots[-1]['id'] if roots else None
        if independent:
            message += '\n\n!file[' + reference['filename'] + '](' + row['markdown_url'] + ')'
        return comment(reference['issue_id'], message, parent, () if independent else (row['id'],))
    except Exception as error:
        raise ValueError('Resource uploaded as ' + row['id'] +
                         '; reconcile the reference before retrying announcement: ' + str(error)) from error


def write_artifact(reference, content, parent=None, message=' ', *, independent=False):
    return _write_resource(reference, content, 'text/html', parent, message, independent=independent)


def publish_shared_artifact(reference, content):
    issue = cli('issue', 'get', reference['issue_id'])
    ids = json.loads(issue['metadata'].get('independent_artifacts', '[]'))
    renewal = []
    for identifier in ids:
        attachment = cli('attachment', 'get', identifier)
        if attachment['filename'] == reference['filename']:
            renewal = ['--attachment-id', attachment['id']]
            break
    with tempfile.TemporaryDirectory(prefix='.featssship-', dir=Path.cwd()) as directory:
        path = Path(directory) / reference['filename']
        path.write_text(content)
        attachment = cli('attachment', 'upload', str(path), '--independent', *renewal)
    return attachment
