-- Persist visibility changes before moving objects across storage access boundaries.
-- +goose Up
ALTER TABLE media_assets DROP CONSTRAINT media_assets_status_check;
ALTER TABLE media_assets ADD CONSTRAINT media_assets_status_check
 CHECK (status IN ('pending','ready','changing_visibility','deleting','deleted'));
ALTER TABLE media_assets ADD COLUMN visibility_target TEXT NOT NULL DEFAULT '';
ALTER TABLE media_assets ADD CONSTRAINT media_assets_visibility_target_check
 CHECK ((status='changing_visibility' AND visibility_target IN ('public','private')
         AND visibility_target<>visibility AND size>0)
     OR (status<>'changing_visibility' AND visibility_target=''));
DROP INDEX media_assets_recovery;
CREATE INDEX media_assets_recovery ON media_assets (updated_at,id)
 WHERE status IN ('pending','changing_visibility','deleting');

-- +goose Down
-- Refuse rollback while visibility changes are pending, preserving recovery intent.
ALTER TABLE media_assets DROP CONSTRAINT media_assets_status_check;
ALTER TABLE media_assets ADD CONSTRAINT media_assets_status_check
 CHECK (status IN ('pending','ready','deleting','deleted'));
ALTER TABLE media_assets DROP COLUMN visibility_target;
DROP INDEX media_assets_recovery;
CREATE INDEX media_assets_recovery ON media_assets (updated_at,id) WHERE status IN ('pending','deleting');
