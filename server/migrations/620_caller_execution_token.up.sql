-- Keep the authenticated caller through asynchronous special-comment execution.
ALTER TABLE comment ADD COLUMN caller_user_id uuid;
ALTER TABLE task_token ALTER COLUMN agent_id DROP NOT NULL;
