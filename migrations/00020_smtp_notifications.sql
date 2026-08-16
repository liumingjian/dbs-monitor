-- +goose Up
CREATE TABLE smtp_channel (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    enabled boolean NOT NULL DEFAULT false,
    host text NOT NULL CHECK (btrim(host) <> ''),
    port integer NOT NULL CHECK (port BETWEEN 1 AND 65535),
    from_address text NOT NULL CHECK (btrim(from_address) <> ''),
    recipient text NOT NULL CHECK (btrim(recipient) <> ''),
    auth_type text NOT NULL CHECK (auth_type IN ('NONE', 'PLAIN', 'LOGIN')),
    username text,
    auth_ciphertext bytea,
    auth_key_version integer,
    tls_mode text NOT NULL CHECK (tls_mode IN ('STARTTLS', 'IMPLICIT')),
    updated_at timestamptz NOT NULL,
    CHECK ((auth_ciphertext IS NULL) = (auth_key_version IS NULL)),
    CHECK ((auth_type = 'NONE' AND username IS NULL AND auth_ciphertext IS NULL)
        OR (auth_type <> 'NONE' AND username IS NOT NULL
            AND btrim(username) <> '' AND auth_ciphertext IS NOT NULL))
);

CREATE TABLE notification_delivery (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_instance_id uuid REFERENCES alert_instance(id) ON DELETE CASCADE,
    event_type text NOT NULL CHECK (event_type IN ('FIRING', 'RECOVERY', 'REPEAT', 'TEST')),
    channel text NOT NULL CHECK (channel IN ('SMTP')),
    target text NOT NULL CHECK (btrim(target) <> ''),
    template_id text,
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'SENT', 'FAILED')),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 3),
    next_attempt_at timestamptz NOT NULL,
    locked_until timestamptz,
    created_at timestamptz NOT NULL,
    completed_at timestamptz,
    CHECK ((status = 'PENDING' AND completed_at IS NULL)
        OR (status IN ('SENT', 'FAILED') AND completed_at IS NOT NULL))
);

CREATE INDEX notification_delivery_due_idx
    ON notification_delivery (next_attempt_at, created_at)
    WHERE status = 'PENDING';
CREATE INDEX notification_delivery_alert_idx
    ON notification_delivery (alert_instance_id, created_at DESC)
    WHERE alert_instance_id IS NOT NULL;

CREATE TABLE notification_attempt (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    notification_id uuid NOT NULL REFERENCES notification_delivery(id) ON DELETE CASCADE,
    attempted_at timestamptz NOT NULL,
    result text NOT NULL CHECK (result IN ('SENT', 'FAILED')),
    failure_reason text,
    retry_count integer NOT NULL CHECK (retry_count BETWEEN 0 AND 2),
    CHECK ((result = 'SENT' AND failure_reason IS NULL)
        OR (result = 'FAILED' AND btrim(failure_reason) <> '')),
    UNIQUE (notification_id, retry_count)
);

ALTER TABLE alert_event
    DROP CONSTRAINT alert_event_kind_check,
    ADD CONSTRAINT alert_event_kind_check CHECK (kind IN (
        'PENDING_STARTED', 'FIRED', 'UPDATED', 'RECOVERED',
        'NO_DATA_ENTERED', 'NO_DATA_EXITED', 'FROZEN', 'UNFROZEN',
        'ACKED', 'IGNORED', 'INSTANCE_REMOVED',
        'NOTIFICATION_SENT', 'NOTIFICATION_FAILED'
    ));
