-- +goose Up
CREATE TABLE app_user (
    id uuid PRIMARY KEY,
    username text NOT NULL UNIQUE,
    password_hash bytea NOT NULL,
    role text NOT NULL CHECK (role IN ('READONLY', 'ALERT_ADMIN', 'PLATFORM_ADMIN'))
);

CREATE TABLE user_session (
    token_hash bytea PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);

CREATE TABLE instance (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    host text NOT NULL,
    port integer NOT NULL CHECK (port BETWEEN 1 AND 65535),
    database_name text NOT NULL,
    username text NOT NULL,
    password text NOT NULL,
    agent_token_hash bytea,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE instance_collect_state (
    instance_id uuid NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    source text NOT NULL CHECK (source IN ('SERVER_DIRECT', 'AGENT')),
    last_success_at timestamptz,
    last_report_at timestamptz,
    last_error_code text,
    last_error_message text,
    PRIMARY KEY (instance_id, source)
);

CREATE TABLE metric_series (
    series_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    instance_id uuid NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    metric_id text NOT NULL CHECK (metric_id IN (
        'pg.availability.reachable',
        'pg.probe.latency_ms',
        'collector.last_success_time',
        'agent.status',
        'host.cpu.usage_percent',
        'host.memory.usage_percent',
        'host.disk.usage_percent',
        'host.disk.free_bytes',
        'host.disk.iops',
        'host.disk.throughput_bytes_per_sec',
        'host.network.bytes_per_sec',
        'pg.connection.total',
        'pg.connection.active',
        'pg.connection.idle_in_transaction',
        'pg.tps',
        'pg.xact.commit_per_sec',
        'pg.xact.rollback_per_sec',
        'pg.tuples.read_per_sec',
        'pg.tuples.write_per_sec',
        'pg.temp.files_per_sec',
        'pg.temp.bytes_per_sec',
        'pg.transaction.long_count',
        'pg.transaction.max_duration_sec',
        'pg.lock.waiting_count',
        'pg.session.blocked_count',
        'pg.query.long_running_count',
        'pg.prepared_xacts.count',
        'pg.replication.role',
        'pg.replication.connection_state',
        'pg.replication.replay_lag_ms',
        'pg.replication.wal_lag_bytes',
        'pg.replication_slot.retained_wal_bytes'
    )),
    labels jsonb NOT NULL DEFAULT '{}',
    labels_key text NOT NULL,
    first_seen timestamptz NOT NULL DEFAULT now(),
    last_seen timestamptz NOT NULL,
    UNIQUE (instance_id, metric_id, labels_key)
);

CREATE TABLE metric_sample (
    series_id bigint NOT NULL,
    ts timestamptz NOT NULL,
    value double precision NOT NULL
) PARTITION BY RANGE (ts);
CREATE INDEX metric_sample_series_ts_idx ON metric_sample (series_id, ts DESC);

CREATE TABLE alert_instance (
    instance_id uuid PRIMARY KEY REFERENCES instance(id) ON DELETE CASCADE,
    metric_id text NOT NULL CHECK (metric_id = 'pg.connection.total'),
    status text NOT NULL CHECK (status IN ('OK', 'PENDING', 'FIRING', 'NO_DATA', 'RECOVERED')),
    breach_count integer NOT NULL DEFAULT 0 CHECK (breach_count >= 0),
    recovery_count integer NOT NULL DEFAULT 0 CHECK (recovery_count >= 0),
    no_data_count integer NOT NULL DEFAULT 0 CHECK (no_data_count >= 0),
    state_before_no_data text CHECK (state_before_no_data IS NULL OR state_before_no_data IN ('OK', 'PENDING', 'FIRING')),
    unavailability text,
    updated_at timestamptz NOT NULL DEFAULT now()
);
