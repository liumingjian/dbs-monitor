-- +goose Up
ALTER TABLE alert_rule
    ADD COLUMN deleted_by uuid REFERENCES app_user(id),
    ADD COLUMN deleted_at timestamptz,
    ADD CONSTRAINT alert_rule_deletion_attribution_pair CHECK (
        (deleted_by IS NULL) = (deleted_at IS NULL)
    );
