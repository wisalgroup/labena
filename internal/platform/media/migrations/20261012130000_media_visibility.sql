-- Add visibility to the existing PostgreSQL media table.
-- +goose Up
-- Existing objects were uploaded privately; do not expose them during upgrade.
ALTER TABLE media_assets ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private'
 CHECK (visibility IN ('public','private'));
ALTER TABLE media_assets ALTER COLUMN visibility SET DEFAULT 'public';
-- +goose Down
ALTER TABLE media_assets DROP COLUMN visibility;
