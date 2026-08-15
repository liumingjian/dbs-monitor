-- +goose Up
ALTER TABLE instance
    ADD COLUMN password_ciphertext bytea,
    ADD COLUMN password_key_version integer NOT NULL DEFAULT 1,
    ADD COLUMN credential_version bigint NOT NULL DEFAULT 1,
    ALTER COLUMN password SET DEFAULT '',
    ADD CONSTRAINT instance_password_key_version_check CHECK (password_key_version > 0),
    ADD CONSTRAINT instance_credential_version_check CHECK (credential_version > 0);
