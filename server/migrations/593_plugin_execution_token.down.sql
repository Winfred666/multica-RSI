UPDATE personal_access_token SET revoked = TRUE
WHERE id IN (SELECT exec_pat_id FROM plugin_installation);
DELETE FROM plugin_secret WHERE key = '__exec_token';
ALTER TABLE plugin_installation DROP COLUMN IF EXISTS exec_pat_id;
