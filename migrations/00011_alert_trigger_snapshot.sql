-- +goose Up
CREATE TABLE alert_trigger_snapshot (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_instance_id uuid NOT NULL UNIQUE REFERENCES alert_instance(id) ON DELETE CASCADE,
    captured_at timestamptz NOT NULL,
    result text NOT NULL CHECK (result IN ('SUCCESS', 'FAILED')),
    original_match_count integer NOT NULL DEFAULT 0 CHECK (original_match_count >= 0),
    truncated boolean NOT NULL DEFAULT false,
    failure_reason text,
    CHECK ((result = 'SUCCESS' AND failure_reason IS NULL)
        OR (result = 'FAILED' AND failure_reason IS NOT NULL))
);

CREATE TABLE alert_trigger_snapshot_session (
    snapshot_id uuid NOT NULL REFERENCES alert_trigger_snapshot(id) ON DELETE CASCADE,
    pid integer NOT NULL CHECK (pid > 0),
    username text,
    database_name text,
    client_address text,
    state text,
    query_started_at timestamptz,
    transaction_started_at timestamptz,
    query_duration_ms bigint CHECK (query_duration_ms IS NULL OR query_duration_ms >= 0),
    transaction_duration_ms bigint CHECK (transaction_duration_ms IS NULL OR transaction_duration_ms >= 0),
    wait_event_type text,
    wait_event text,
    blocking_pids integer[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (snapshot_id, pid)
);

ALTER TABLE alert_event
    ADD COLUMN trigger_snapshot_id uuid REFERENCES alert_trigger_snapshot(id);
