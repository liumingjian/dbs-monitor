-- +goose Up
ALTER TABLE app_user
    ADD COLUMN enabled boolean NOT NULL DEFAULT true,
    ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
