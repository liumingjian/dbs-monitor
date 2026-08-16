-- +goose Up
ALTER TABLE smtp_channel
    ADD COLUMN updated_by uuid REFERENCES app_user(id);
