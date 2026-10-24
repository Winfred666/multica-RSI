UPDATE issue_wakeup SET enabled = FALSE, next_fire_at = NULL, disabled_at = now(), revision = revision + 1
WHERE system_rule = 'child_done';

UPDATE issue_wakeup_receipt SET processed_at = now()
WHERE processed_at IS NULL AND wakeup_id IN (SELECT id FROM issue_wakeup WHERE system_rule = 'child_done');

UPDATE agent_task_queue SET status = 'cancelled', completed_at = now()
WHERE context->>'wakeup_system' = 'child_done' AND status IN ('queued', 'deferred');
