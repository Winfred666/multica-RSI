-- name: ListRemoteSOPComments :many
SELECT c.* FROM comment c
JOIN issue i ON i.id = c.issue_id AND i.workspace_id = c.workspace_id
JOIN agent a ON a.id = i.assignee_id AND i.assignee_type = 'agent'
WHERE a.runtime_id = $1 AND c.content LIKE '/note !sop %' AND c.deleted_at IS NULL
ORDER BY c.created_at;

-- name: TakeRemoteSOPComment :one
DELETE FROM comment WHERE id = $1 AND workspace_id = $2 AND content LIKE '/note !sop %'
RETURNING *;

-- name: CreateRemoteSOPLog :one
INSERT INTO agent_task_queue (id, agent_id, runtime_id, issue_id, status, completed_at, context, trigger_summary)
VALUES ($1, $2, $3, $4, 'completed', now(), '{"type":"remote_sop"}', 'Remote SOP')
RETURNING *;
