-- +goose Up
CREATE TABLE instance_identity (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (name <> ''),
    removed_at timestamptz
);

INSERT INTO instance_identity (id, name)
SELECT id, name FROM instance;

ALTER TABLE instance
    ADD CONSTRAINT instance_identity_fkey
        FOREIGN KEY (id) REFERENCES instance_identity(id);

ALTER TABLE alert_instance
    DROP CONSTRAINT alert_instance_instance_id_fkey,
    ADD CONSTRAINT alert_instance_instance_id_fkey
        FOREIGN KEY (instance_id) REFERENCES instance_identity(id);

ALTER TABLE metric_series
    DROP CONSTRAINT metric_series_instance_id_fkey,
    ADD CONSTRAINT metric_series_instance_id_fkey
        FOREIGN KEY (instance_id) REFERENCES instance_identity(id);

ALTER TABLE alert_event
    DROP CONSTRAINT alert_event_kind_check,
    ADD CONSTRAINT alert_event_kind_check CHECK (kind IN (
        'PENDING_STARTED', 'FIRED', 'UPDATED', 'RECOVERED',
        'NO_DATA_ENTERED', 'NO_DATA_EXITED', 'FROZEN', 'UNFROZEN',
        'ACKED', 'IGNORED', 'INSTANCE_REMOVED'
    ));
