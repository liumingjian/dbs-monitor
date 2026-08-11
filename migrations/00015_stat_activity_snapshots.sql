-- +goose Up
CREATE TABLE long_query_sample_snapshot (
    instance_id uuid NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    sampled_at timestamptz NOT NULL,
    original_count integer NOT NULL CHECK (original_count >= 0),
    truncated boolean NOT NULL,
    PRIMARY KEY (instance_id, sampled_at)
);

CREATE TABLE long_query_sample (
    instance_id uuid NOT NULL,
    sampled_at timestamptz NOT NULL,
    pid integer NOT NULL,
    username text,
    database_name text,
    client_address text,
    state text,
    query_started_at timestamptz NOT NULL,
    transaction_started_at timestamptz,
    query_duration_ms bigint NOT NULL CHECK (query_duration_ms >= 0),
    transaction_duration_ms bigint CHECK (transaction_duration_ms >= 0),
    wait_event_type text,
    wait_event text,
    blocking_pids integer[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (instance_id, sampled_at, pid),
    FOREIGN KEY (instance_id, sampled_at)
        REFERENCES long_query_sample_snapshot(instance_id, sampled_at) ON DELETE CASCADE
);
CREATE INDEX long_query_sample_instance_sampled_at_idx
    ON long_query_sample (instance_id, sampled_at DESC);

CREATE TABLE instance_session_snapshot (
    instance_id uuid PRIMARY KEY REFERENCES instance(id) ON DELETE CASCADE,
    sampled_at timestamptz NOT NULL,
    original_count integer NOT NULL CHECK (original_count >= 0),
    truncated boolean NOT NULL
);

CREATE TABLE instance_session_snapshot_entry (
    instance_id uuid NOT NULL REFERENCES instance_session_snapshot(instance_id) ON DELETE CASCADE,
    pid integer NOT NULL,
    username text,
    database_name text,
    client_address text,
    state text,
    query_started_at timestamptz,
    transaction_started_at timestamptz,
    query_duration_ms bigint CHECK (query_duration_ms >= 0),
    transaction_duration_ms bigint CHECK (transaction_duration_ms >= 0),
    wait_event_type text,
    wait_event text,
    blocking_pids integer[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (instance_id, pid)
);
