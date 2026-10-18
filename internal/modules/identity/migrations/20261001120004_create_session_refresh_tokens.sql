-- Identity owns session_refresh_tokens. Business values are validated in Go.
-- +goose Up
CREATE TABLE session_refresh_tokens (
 id UUID PRIMARY KEY,
 session_id UUID NOT NULL REFERENCES sessions(id),
 token_hash BYTEA NOT NULL UNIQUE,
 created_at TIMESTAMPTZ NOT NULL,
 expires_at TIMESTAMPTZ, -- NULL means no time-based expiry; consumption/revocation still apply.
 consumed_at TIMESTAMPTZ
);
CREATE INDEX session_refresh_tokens_session_id_idx ON session_refresh_tokens (session_id);

-- +goose Down
DROP TABLE session_refresh_tokens;
