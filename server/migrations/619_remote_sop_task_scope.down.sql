CREATE OR REPLACE FUNCTION set_agent_task_comment_thread() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 NEW.comment_thread_id := COALESCE(NULLIF(NEW.context->>'wakeup_id','')::uuid,comment_thread_root_id(NEW.trigger_comment_id));
 RETURN NEW;
END
$$;
