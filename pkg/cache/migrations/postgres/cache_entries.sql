-- +goose Up
CREATE TABLE "{{.Table}}" (
    namespace TEXT COLLATE "C" NOT NULL,
    cache_key TEXT COLLATE "C" NOT NULL,
    value BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (namespace, cache_key)
);
CREATE INDEX "{{.Table}}_expiry" ON "{{.Table}}" (namespace, expires_at, cache_key);

-- +goose Down
DROP TABLE "{{.Table}}";
