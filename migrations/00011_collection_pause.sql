-- +goose Up
ALTER TABLE instance_collection_config
    ADD COLUMN collection_paused boolean NOT NULL DEFAULT false,
    ADD COLUMN collection_pause_updated_by uuid REFERENCES app_user(id),
    ADD COLUMN collection_pause_updated_at timestamptz,
    ADD COLUMN collection_pause_reason text,
    ADD CONSTRAINT instance_collection_pause_actor_time_check CHECK (
        (collection_pause_updated_by IS NULL) = (collection_pause_updated_at IS NULL)
    );

ALTER TABLE alert_event
    DROP CONSTRAINT alert_event_kind_check,
    ADD CONSTRAINT alert_event_kind_check CHECK (kind IN (
        'PENDING_STARTED', 'FIRED', 'UPDATED', 'RECOVERED',
        'NO_DATA_ENTERED', 'NO_DATA_EXITED', 'FROZEN', 'UNFROZEN'
    )),
    ADD COLUMN actor_id uuid REFERENCES app_user(id);
