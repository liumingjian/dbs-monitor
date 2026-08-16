-- +goose Up
ALTER TABLE webhook_target
    ADD COLUMN updated_by uuid REFERENCES app_user(id);
