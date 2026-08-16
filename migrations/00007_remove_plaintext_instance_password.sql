-- +goose Up
ALTER TABLE instance
    ALTER COLUMN password_ciphertext SET NOT NULL,
    ALTER COLUMN password_key_version SET NOT NULL,
    ADD CONSTRAINT instance_password_key_version_check CHECK (password_key_version > 0),
    ADD CONSTRAINT instance_credential_version_check CHECK (credential_version > 0),
    DROP COLUMN password;
