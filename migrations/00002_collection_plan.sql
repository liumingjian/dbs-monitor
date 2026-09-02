-- +goose Up
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
