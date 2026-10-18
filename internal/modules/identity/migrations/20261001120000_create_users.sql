-- Identity owns accounts. Business values are validated in Go.
-- Contact uniqueness remains reserved after deletion until explicitly released.
-- +goose Up
CREATE TABLE users (
 id UUID PRIMARY KEY,
 email TEXT NOT NULL UNIQUE,
 email_verified_at TIMESTAMPTZ,
 phone TEXT UNIQUE,
 phone_verified_at TIMESTAMPTZ,
 name TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'active',
 last_active_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL,
 deleted_at TIMESTAMPTZ
);

-- +goose Down
DROP TABLE users;
