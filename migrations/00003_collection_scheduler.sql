-- +goose Up
ALTER TABLE collection_task_config
    ADD COLUMN updated_by uuid REFERENCES app_user(id);

CREATE TABLE instance_collection_task_state (
    instance_id uuid NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    task_id text NOT NULL CHECK (task_id <> ''),
    last_due_at timestamptz,
    last_started_at timestamptz,
    last_finished_at timestamptz,
    last_success_at timestamptz,
    last_result text CHECK (last_result IS NULL OR last_result IN (
        'SUCCESS',
        'FAILED',
        'TIMED_OUT',
        'SKIPPED_BACKPRESSURE',
        'BACKOFF'
    )),
    consecutive_failures integer NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    next_eligible_at timestamptz,
    last_error_code text,
    last_error_message text,
    PRIMARY KEY (instance_id, task_id)
);

CREATE TABLE instance_collection_connection_state (
    instance_id uuid PRIMARY KEY REFERENCES instance(id) ON DELETE CASCADE,
    consecutive_failures integer NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    next_eligible_at timestamptz,
    last_error_code text,
    last_error_message text
);
