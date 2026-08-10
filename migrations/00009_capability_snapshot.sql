-- +goose Up
CREATE TABLE instance_capability_snapshot (
    instance_id uuid PRIMARY KEY REFERENCES instance(id) ON DELETE CASCADE,
    observed_at timestamptz NOT NULL,
    states jsonb NOT NULL CHECK (jsonb_typeof(states) = 'object')
);
