-- +goose Up
ALTER TABLE platform_event
    DROP CONSTRAINT platform_event_kind_check,
    ADD CONSTRAINT platform_event_kind_check CHECK (kind IN (
        'LOGIN_SUCCEEDED',
        'LOGIN_FAILED',
        'USER_CREATED',
        'USER_STATUS_CHANGED',
        'USER_STATUS_CHANGE_REJECTED',
        'USER_ROLE_CHANGED',
        'USER_PASSWORD_RESET',
        'INSTANCE_CREDENTIAL_UPDATED',
        'INSTANCE_REMOVED',
        'MASTER_KEY_ROTATED',
        'DIAGNOSTIC_BUNDLE_RECLAIMED',
        'NOTIFICATION_SNAPSHOT_RECLAIMED'
    ));
