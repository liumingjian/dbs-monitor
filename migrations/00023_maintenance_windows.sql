-- +goose Up
CREATE TABLE maintenance_window (
    id uuid PRIMARY KEY,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    created_by uuid NOT NULL REFERENCES app_user(id),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz,
    CHECK (starts_at < ends_at)
);

CREATE TABLE maintenance_window_instance (
    maintenance_window_id uuid NOT NULL REFERENCES maintenance_window(id) ON DELETE CASCADE,
    instance_id uuid NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    PRIMARY KEY (maintenance_window_id, instance_id)
);

CREATE INDEX maintenance_window_instance_lookup_idx
    ON maintenance_window_instance (instance_id, maintenance_window_id);

ALTER TABLE alert_instance
    ADD COLUMN in_maintenance boolean NOT NULL DEFAULT false,
    ADD COLUMN maintenance_window_id uuid REFERENCES maintenance_window(id);

ALTER TABLE alert_event
    DROP CONSTRAINT alert_event_kind_check,
    ADD CONSTRAINT alert_event_kind_check CHECK (kind IN (
        'PENDING_STARTED', 'FIRED', 'UPDATED', 'RECOVERED',
        'NO_DATA_ENTERED', 'NO_DATA_EXITED', 'FROZEN', 'UNFROZEN',
        'ACKED', 'IGNORED', 'INSTANCE_REMOVED',
        'NOTIFICATION_SENT', 'NOTIFICATION_FAILED', 'MAINTENANCE_SUPPRESSED'
    )),
    ADD COLUMN in_maintenance boolean NOT NULL DEFAULT false,
    ADD COLUMN maintenance_window_id uuid REFERENCES maintenance_window(id);
