ALTER TABLE task_token ALTER COLUMN agent_id SET NOT NULL;
ALTER TABLE comment DROP COLUMN caller_user_id;
