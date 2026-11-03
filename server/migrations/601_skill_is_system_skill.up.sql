ALTER TABLE skill ADD COLUMN is_system_skill BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE skill SET is_system_skill = TRUE
WHERE name IN ('multica-issue-turn', 'multica-quick-create', 'multica-surface-delivery', 'multica-onboarding', 'multica-platform');
