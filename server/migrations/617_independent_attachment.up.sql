ALTER TABLE attachment ADD COLUMN independent boolean NOT NULL DEFAULT false;
ALTER TABLE attachment ADD CONSTRAINT independent_attachment_has_no_owner CHECK (
    NOT independent OR (
        issue_id IS NULL AND comment_id IS NULL AND task_id IS NULL
        AND chat_session_id IS NULL AND chat_message_id IS NULL AND source_context_id IS NULL
    )
);
