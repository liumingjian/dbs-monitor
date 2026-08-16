-- +goose Up
ALTER TABLE maintenance_window
    ADD COLUMN updated_by uuid REFERENCES app_user(id),
    ADD COLUMN ended_by uuid REFERENCES app_user(id),
    ADD COLUMN deleted_by uuid REFERENCES app_user(id);

UPDATE maintenance_window SET updated_by = created_by;

ALTER TABLE maintenance_window
    ALTER COLUMN updated_by SET NOT NULL,
    ADD CONSTRAINT maintenance_window_deletion_attribution_pair CHECK (
        (deleted_by IS NULL) = (deleted_at IS NULL)
    );
