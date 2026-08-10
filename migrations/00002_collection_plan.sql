-- +goose Up
ALTER TABLE metric_series DROP CONSTRAINT metric_series_metric_id_check;
ALTER TABLE metric_series ADD CONSTRAINT metric_series_metric_id_check CHECK (metric_id IN (
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
));

CREATE TABLE instance_collection_config (
    instance_id uuid PRIMARY KEY REFERENCES instance(id) ON DELETE CASCADE,
    agent_metrics_enabled boolean NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO instance_collection_config (instance_id)
SELECT id FROM instance;

CREATE TABLE collection_task_config (
    instance_id uuid NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    task_id text NOT NULL CHECK (task_id <> ''),
    interval_seconds integer NOT NULL CHECK (interval_seconds >= 5),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (instance_id, task_id)
);
