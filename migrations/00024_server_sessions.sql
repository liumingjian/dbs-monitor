-- +goose Up
ALTER TABLE user_session
    ADD COLUMN created_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN last_seen_at timestamptz NOT NULL DEFAULT now();

CREATE INDEX user_session_user_id_idx ON user_session (user_id);
