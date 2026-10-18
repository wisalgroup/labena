-- Identity owns auth_identities. Business values are validated in Go.
-- +goose Up
CREATE TABLE auth_identities (
 id UUID PRIMARY KEY,
 user_id UUID NOT NULL REFERENCES users(id),
 provider TEXT NOT NULL,
 provider_subject TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL,
 last_authenticated_at TIMESTAMPTZ,
 UNIQUE (provider, provider_subject)
);
CREATE INDEX auth_identities_user_id_idx ON auth_identities (user_id);

-- +goose Down
DROP TABLE auth_identities;
