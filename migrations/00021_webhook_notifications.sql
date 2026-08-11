-- +goose Up
CREATE TABLE webhook_target (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (btrim(name) <> ''),
    enabled boolean NOT NULL DEFAULT true,
    url text NOT NULL CHECK (btrim(url) <> ''),
    signing_value_ciphertext bytea NOT NULL,
    signature_header_ciphertext bytea NOT NULL,
    signing_key_version integer NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

ALTER TABLE notification_delivery
    DROP CONSTRAINT notification_delivery_channel_check,
    ADD CONSTRAINT notification_delivery_channel_check CHECK (channel IN ('SMTP', 'WEBHOOK')),
    ADD COLUMN channel_target_id uuid REFERENCES webhook_target(id) ON DELETE SET NULL;

CREATE INDEX notification_delivery_channel_status_idx
    ON notification_delivery (channel, channel_target_id, completed_at DESC)
    WHERE status IN ('SENT', 'FAILED');
