"""Wait for one member inbox notification, using the native issue-level read API."""
import json
import time
from urllib.request import Request, urlopen
from uuid import UUID


def wait(server, token, target):
    def request(path, method='GET'):
        req = Request(server.rstrip('/') + '/api/inbox' + path, method=method,
                      headers={'Authorization': 'Bearer ' + token, 'X-Workspace-ID': target})
        with urlopen(req, timeout=30) as response:
            return json.load(response)

    while True:
        unread = [item for item in request('') if not item['read'] and not item['archived']]
        if unread:
            oldest = unread[-1]  # Native inbox list is newest first.
            # Native read also acknowledges unread siblings on this issue.
            item = request('/' + str(UUID(oldest['id'])) + '/read', 'POST')
            event = {key: item[key] for key in (
                'id', 'workspace_id', 'issue_id', 'title', 'type', 'actor_type',
                'actor_id', 'body', 'details', 'issue_status', 'read',
            )}
            event['body_truncated'] = len(item['body'] or '') > 500
            event['body'] = item['body'][:500] if item['body'] else item['body']
            event['details'] = {k: v for k, v in (item['details'] or {}).items() if k == 'comment_id'}
            event['next'] = ('Reply or steer briefly if needed; rerun this inbox-wait command '
                             'unless target work is complete or acceptable, then stop.')
            return event
        time.sleep(5)
