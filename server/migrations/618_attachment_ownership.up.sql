ALTER TABLE attachment DROP CONSTRAINT IF EXISTS independent_attachment_has_no_owner;
ALTER TABLE attachment DROP COLUMN IF EXISTS independent;
