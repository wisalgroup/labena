-- platform: postgres notifications notifications
-- +goose Up
CREATE TABLE notifications (
 id UUID PRIMARY KEY,
 recipient_type TEXT NOT NULL CHECK (recipient_type ~ '^[a-z][a-z0-9_.-]{0,63}$'),
 recipient_id TEXT NOT NULL CHECK (octet_length(recipient_id) BETWEEN 1 AND 200 AND recipient_id=btrim(recipient_id)),
 type TEXT NOT NULL CHECK (octet_length(type) BETWEEN 1 AND 100 AND type=btrim(type)),
 key TEXT NOT NULL CHECK (octet_length(key) BETWEEN 1 AND 200),
 data JSONB NOT NULL CHECK (jsonb_typeof(data)='object' AND octet_length(data::text)<=65536),
 read_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL,
 UNIQUE (recipient_type,recipient_id,key),
 CHECK (updated_at>=created_at AND (read_at IS NULL OR read_at>=created_at))
);
CREATE INDEX notifications_recipient ON notifications (recipient_type,recipient_id,created_at DESC,id DESC);
CREATE INDEX notifications_unread ON notifications (recipient_type,recipient_id) WHERE read_at IS NULL;
CREATE TABLE notification_deliveries (
 id UUID PRIMARY KEY,
 recipient_type TEXT NOT NULL CHECK (recipient_type ~ '^[a-z][a-z0-9_.-]{0,63}$'),
 recipient_id TEXT NOT NULL CHECK (octet_length(recipient_id) BETWEEN 1 AND 200 AND recipient_id=btrim(recipient_id)),
 key TEXT NOT NULL CHECK (octet_length(key) BETWEEN 1 AND 200),
 type TEXT NOT NULL CHECK (octet_length(type) BETWEEN 1 AND 100 AND type=btrim(type)),
 channel TEXT NOT NULL CHECK (channel IN ('database','email','push')),
 destination TEXT NOT NULL DEFAULT '',
 push_ticket_id TEXT NOT NULL DEFAULT '',
 push_accepted_at TIMESTAMPTZ,
 subscription_version BIGINT NOT NULL DEFAULT 0,
 payload JSONB NOT NULL CHECK (jsonb_typeof(payload)='object' AND octet_length(payload::text)<=131072),
 status TEXT NOT NULL CHECK (status IN ('pending','sending','sent','failed','skipped','awaiting_receipt')),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 8),
 available_at TIMESTAMPTZ NOT NULL,
 lease_token UUID,
 lease_until TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL,
 UNIQUE (recipient_type,recipient_id,key,channel,destination),
 CHECK (channel<>'database' OR (payload ? 'data' AND jsonb_typeof(payload->'data')='object' AND octet_length((payload->'data')::text)<=65536)),
 CHECK ((push_ticket_id='' AND push_accepted_at IS NULL) OR (channel='push' AND octet_length(push_ticket_id) BETWEEN 1 AND 200 AND push_accepted_at IS NOT NULL)),
 CHECK (status<>'awaiting_receipt' OR push_ticket_id<>''),
 CHECK ((status='sending' AND lease_token IS NOT NULL AND lease_until IS NOT NULL) OR (status<>'sending' AND lease_token IS NULL AND lease_until IS NULL))
);
CREATE INDEX notification_deliveries_due ON notification_deliveries (available_at,id) WHERE status IN ('pending','sending','awaiting_receipt');
CREATE INDEX notification_deliveries_status ON notification_deliveries (status,created_at DESC,id DESC);
CREATE TABLE notification_subscriptions (
 id UUID PRIMARY KEY,
 recipient_type TEXT NOT NULL,
 recipient_id TEXT NOT NULL,
 installation_id UUID NOT NULL,
 session_id TEXT NOT NULL,
 project TEXT NOT NULL,
 registration TEXT NOT NULL CHECK (octet_length(registration) BETWEEN 1 AND 4096),
 registration_hash TEXT NOT NULL,
 version BIGINT NOT NULL DEFAULT 1 CHECK (version>0),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 disabled_at TIMESTAMPTZ,
 UNIQUE(project,installation_id)
);
CREATE UNIQUE INDEX notification_subscription_registration ON notification_subscriptions(project,registration_hash) WHERE disabled_at IS NULL;
CREATE INDEX notification_subscription_recipient ON notification_subscriptions(recipient_type,recipient_id,project) WHERE disabled_at IS NULL;
CREATE INDEX notification_subscription_stale ON notification_subscriptions(updated_at) WHERE disabled_at IS NULL;
-- +goose Down
DROP TABLE notification_subscriptions;
DROP TABLE notification_deliveries;
DROP TABLE notifications;
