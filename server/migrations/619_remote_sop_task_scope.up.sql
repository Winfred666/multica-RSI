-- Commands keep independent pending slots; model tasks still coalesce by thread.
CREATE OR REPLACE FUNCTION set_agent_task_comment_thread() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 NEW.comment_thread_id := CASE WHEN NEW.context->>'type' = 'remote_sop' THEN NEW.id ELSE COALESCE(NULLIF(NEW.context->>'wakeup_id','')::uuid,comment_thread_root_id(NEW.trigger_comment_id)) END;
 RETURN NEW;
END
$$;
