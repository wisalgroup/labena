-- Identity owns sessions. Business values are validated in Go.
-- +goose Up
CREATE TABLE sessions (
 id UUID PRIMARY KEY,
 user_id UUID NOT NULL REFERENCES users(id),
 platform TEXT NOT NULL,
 device_name TEXT,
 created_at TIMESTAMPTZ NOT NULL,
 last_used_at TIMESTAMPTZ NOT NULL,
 expires_at TIMESTAMPTZ, -- NULL means no absolute deadline.
 revoked_at TIMESTAMPTZ
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- +goose Down
DROP TABLE sessions;
