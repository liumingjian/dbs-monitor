-- +goose Up
ALTER TABLE instance
    ADD COLUMN password_ciphertext bytea,
    ADD COLUMN password_key_version integer,
    ADD COLUMN credential_version bigint NOT NULL DEFAULT 1;
