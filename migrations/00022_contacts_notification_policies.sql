-- +goose Up
CREATE TABLE notification_contact (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (btrim(name) <> ''),
    email text NOT NULL CHECK (btrim(email) <> ''),
    external_id text,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (external_id IS NULL OR btrim(external_id) <> '')
);

CREATE TABLE notification_contact_group (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (btrim(name) <> ''),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE notification_contact_group_member (
    group_id uuid NOT NULL REFERENCES notification_contact_group(id) ON DELETE CASCADE,
    contact_id uuid NOT NULL REFERENCES notification_contact(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, contact_id)
);

ALTER TABLE notification_policy
    ADD COLUMN severity_filter text[] NOT NULL DEFAULT ARRAY['critical', 'warning', 'info']::text[],
    ADD COLUMN notify_on_fire boolean NOT NULL DEFAULT true,
    ADD COLUMN notify_on_recovery boolean NOT NULL DEFAULT true,
    ADD COLUMN repeat_interval integer NOT NULL DEFAULT 3600
        CHECK (repeat_interval BETWEEN 900 AND 86400),
    ADD COLUMN template_id text,
    ADD CONSTRAINT notification_policy_severity_filter_check CHECK (
        cardinality(severity_filter) > 0
        AND severity_filter <@ ARRAY['critical', 'warning', 'info']::text[]
    );

CREATE TABLE notification_policy_contact (
    policy_id uuid NOT NULL REFERENCES notification_policy(id) ON DELETE CASCADE,
    contact_id uuid NOT NULL REFERENCES notification_contact(id) ON DELETE CASCADE,
    PRIMARY KEY (policy_id, contact_id)
);

CREATE TABLE notification_policy_contact_group (
    policy_id uuid NOT NULL REFERENCES notification_policy(id) ON DELETE CASCADE,
    group_id uuid NOT NULL REFERENCES notification_contact_group(id) ON DELETE CASCADE,
    PRIMARY KEY (policy_id, group_id)
);

CREATE TABLE notification_policy_channel (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    policy_id uuid NOT NULL REFERENCES notification_policy(id) ON DELETE CASCADE,
    channel text NOT NULL CHECK (channel IN ('SMTP', 'WEBHOOK')),
    channel_target_id uuid REFERENCES webhook_target(id) ON DELETE CASCADE,
    CHECK ((channel = 'SMTP' AND channel_target_id IS NULL)
        OR (channel = 'WEBHOOK' AND channel_target_id IS NOT NULL))
);

CREATE UNIQUE INDEX notification_policy_smtp_channel_idx
    ON notification_policy_channel (policy_id)
    WHERE channel = 'SMTP';
CREATE UNIQUE INDEX notification_policy_webhook_channel_idx
    ON notification_policy_channel (policy_id, channel_target_id)
    WHERE channel = 'WEBHOOK';

INSERT INTO notification_policy_channel (policy_id, channel)
SELECT id, 'SMTP' FROM notification_policy WHERE is_default
ON CONFLICT DO NOTHING;
