-- name: ListRemoteSOPComments :many
SELECT c.* FROM comment c
JOIN issue i ON i.id = c.issue_id AND i.workspace_id = c.workspace_id
JOIN agent a ON a.id = i.assignee_id AND i.assignee_type = 'agent'
WHERE a.runtime_id = $1 AND c.content LIKE '/note !sop-remote %' AND c.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM agent_task_queue t WHERE t.id = c.id)
ORDER BY c.created_at;

-- name: TakeRemoteSOPComment :one
SELECT * FROM comment WHERE id = $1 AND workspace_id = $2 AND content LIKE '/note !sop-remote %'
FOR UPDATE;

-- name: CreateRemoteSOPLog :one
INSERT INTO agent_task_queue (id, agent_id, runtime_id, issue_id, status, completed_at, context, trigger_summary, originator_user_id, accountable_user_id)
VALUES ($1, $2, $3, $4, 'completed', now(), '{"type":"remote_sop"}', 'Remote SOP', $5, $5)
RETURNING *;

-- name: GetRemoteSOPWorkDir :one
SELECT work_dir FROM agent_task_queue
WHERE runtime_id = $1 AND agent_id = $2 AND issue_id = $3
  AND work_dir IS NOT NULL AND work_dir <> ''
ORDER BY created_at DESC
LIMIT 1;
