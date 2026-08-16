-- +goose Up
ALTER TABLE alert_rule
    ADD COLUMN created_by uuid REFERENCES app_user(id),
    ADD COLUMN updated_by uuid REFERENCES app_user(id);

ALTER TABLE alert_rule_version
    ADD COLUMN created_by uuid REFERENCES app_user(id);
