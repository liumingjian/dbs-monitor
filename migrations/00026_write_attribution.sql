-- +goose Up
ALTER TABLE app_user
    ADD COLUMN created_by uuid REFERENCES app_user(id),
    ADD COLUMN enabled_updated_by uuid REFERENCES app_user(id),
    ADD COLUMN enabled_updated_at timestamptz,
    ADD COLUMN role_updated_by uuid REFERENCES app_user(id),
    ADD COLUMN role_updated_at timestamptz,
    ADD CONSTRAINT app_user_enabled_attribution_check CHECK (
        (enabled_updated_by IS NULL) = (enabled_updated_at IS NULL)
    ),
    ADD CONSTRAINT app_user_role_attribution_check CHECK (
        (role_updated_by IS NULL) = (role_updated_at IS NULL)
    );

ALTER TABLE instance
    ADD COLUMN created_by uuid REFERENCES app_user(id),
    ADD COLUMN credential_updated_by uuid REFERENCES app_user(id),
    ADD COLUMN credential_updated_at timestamptz,
    ADD CONSTRAINT instance_credential_attribution_check CHECK (
        (credential_updated_by IS NULL) = (credential_updated_at IS NULL)
    );

CREATE TABLE platform_event (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN (
        'LOGIN_SUCCEEDED',
        'LOGIN_FAILED',
        'USER_CREATED',
        'USER_STATUS_CHANGED',
        'USER_ROLE_CHANGED',
        'USER_PASSWORD_RESET',
        'INSTANCE_CREDENTIAL_UPDATED',
        'INSTANCE_REMOVED',
        'MASTER_KEY_ROTATED'
    )),
    occurred_at timestamptz NOT NULL,
    actor_id uuid REFERENCES app_user(id),
    actor_subject text,
    subject_id uuid,
    CONSTRAINT platform_event_actor_check CHECK (
        num_nonnulls(actor_id, actor_subject) = 1
        AND (actor_subject IS NULL OR length(btrim(actor_subject)) > 0)
    )
);

CREATE INDEX platform_event_kind_occurred_idx
    ON platform_event (kind, occurred_at DESC, id DESC);
