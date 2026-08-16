-- +goose Up
ALTER TABLE notification_policy
    DROP CONSTRAINT notification_policy_repeat_interval_check,
    ADD CONSTRAINT notification_policy_repeat_interval_check
        CHECK (repeat_interval BETWEEN 1 AND 86400);
