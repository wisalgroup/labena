-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE `{{.Table}}` (
    namespace VARBINARY(255) NOT NULL,
    cache_key VARBINARY(255) NOT NULL,
    value LONGBLOB NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    PRIMARY KEY (namespace, cache_key),
    INDEX `{{.Table}}_expiry` (namespace, expires_at, cache_key)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE `{{.Table}}`;
