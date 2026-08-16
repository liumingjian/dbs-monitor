-- +goose Up
CREATE TABLE query_statistics_snapshot (
    instance_id uuid NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    sampled_at timestamptz NOT NULL,
    PRIMARY KEY (instance_id, sampled_at)
);

CREATE TABLE query_statistics_snapshot_entry (
    instance_id uuid NOT NULL,
    sampled_at timestamptz NOT NULL,
    queryid bigint NOT NULL,
    database_oid oid NOT NULL,
    user_oid oid NOT NULL,
    calls bigint NOT NULL CHECK (calls >= 0),
    total_exec_time_ms double precision NOT NULL CHECK (total_exec_time_ms >= 0),
    PRIMARY KEY (instance_id, sampled_at, queryid, database_oid, user_oid),
    FOREIGN KEY (instance_id, sampled_at)
        REFERENCES query_statistics_snapshot(instance_id, sampled_at) ON DELETE CASCADE
);
CREATE INDEX query_statistics_snapshot_instance_sampled_at_idx
    ON query_statistics_snapshot (instance_id, sampled_at DESC);
