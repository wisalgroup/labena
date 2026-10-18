-- Identity owns verification_challenges. Business values are validated in Go.
-- +goose Up
CREATE TABLE verification_challenges (
 id UUID PRIMARY KEY,
 user_id UUID REFERENCES users(id),
 purpose TEXT NOT NULL,
 channel TEXT NOT NULL,
 destination TEXT NOT NULL,
 code_digest BYTEA NOT NULL,
 attempt_count INTEGER NOT NULL DEFAULT 0,
 max_attempts INTEGER NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 consumed_at TIMESTAMPTZ,
 invalidated_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX verification_challenges_expires_at_idx ON verification_challenges (expires_at);
CREATE INDEX verification_challenges_user_id_idx ON verification_challenges (user_id);
CREATE INDEX verification_challenges_destination_idx ON verification_challenges (destination, purpose);

-- +goose Down
DROP TABLE verification_challenges;
