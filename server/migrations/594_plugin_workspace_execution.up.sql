CREATE TABLE IF NOT EXISTS plugin_workspace_execution (
    workspace_id UUID NOT NULL,
    pat_id UUID NOT NULL,
    ciphertext BYTEA NOT NULL
);

-- Hosted execution now requires an explicitly supplied workspace credential.
UPDATE personal_access_token SET revoked = TRUE
WHERE id IN (SELECT exec_pat_id FROM plugin_installation);
DELETE FROM plugin_secret WHERE key = '__exec_token';
ALTER TABLE plugin_installation DROP COLUMN IF EXISTS exec_pat_id;
