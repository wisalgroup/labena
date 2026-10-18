-- platform: postgres media media_assets
-- +goose Up
CREATE TABLE media_assets (
 id UUID PRIMARY KEY,
 owner_type TEXT NOT NULL CHECK (owner_type ~ '^[a-z][a-z0-9_.-]{0,63}$'),
 owner_id TEXT NOT NULL CHECK (octet_length(owner_id) BETWEEN 1 AND 200 AND owner_id=btrim(owner_id)),
 collection TEXT NOT NULL DEFAULT 'default' CHECK (collection ~ '^[a-z][a-z0-9_.-]{0,63}$'),
 disk TEXT NOT NULL CHECK (length(disk)>0),
 key TEXT NOT NULL CHECK (length(key)>0),
 filename TEXT NOT NULL CHECK (octet_length(filename) BETWEEN 1 AND 255),
 content_type TEXT NOT NULL,
 size BIGINT NOT NULL DEFAULT 0 CHECK (size>=0),
 status TEXT NOT NULL CHECK (status IN ('pending','ready','deleting','deleted')),
 created_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL,
 UNIQUE (disk,key),
 CHECK (status<>'ready' OR size>0)
);
CREATE INDEX media_assets_owner ON media_assets (owner_type,owner_id,created_at DESC,id DESC) WHERE status='ready';
CREATE INDEX media_assets_collection ON media_assets (owner_type,owner_id,collection,created_at DESC,id DESC) WHERE status='ready';
CREATE INDEX media_assets_recovery ON media_assets (updated_at,id) WHERE status IN ('pending','deleting');
-- +goose Down
DROP TABLE media_assets;
