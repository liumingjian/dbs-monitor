-- +goose Up
CREATE TABLE performance_event (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_instance_id uuid NOT NULL UNIQUE REFERENCES alert_instance(id) ON DELETE CASCADE,
    event_type text NOT NULL CHECK (event_type IN (
        'LOCK_BLOCKING',
        'LONG_TRANSACTION',
        'IDLE_IN_TRANSACTION',
        'ACTIVE_SESSIONS_HIGH',
        'REPLICATION_LAG',
        'TEMP_FILES_SURGE'
    )),
    derived_at timestamptz NOT NULL
);

CREATE INDEX performance_event_derived_idx
    ON performance_event (derived_at DESC, id);

CREATE INDEX alert_instance_recovered_retention_idx
    ON alert_instance (recovered_at)
    WHERE recovered_at IS NOT NULL;
