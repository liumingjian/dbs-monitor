-- name: GetSMTPChannel :one
SELECT * FROM smtp_channel WHERE singleton;

-- name: GetSMTPChannelForKeyRotation :one
SELECT * FROM smtp_channel WHERE singleton AND auth_key_version IS NOT NULL FOR UPDATE;

-- name: UpdateSMTPChannelAuthKey :exec
UPDATE smtp_channel
SET auth_ciphertext = $1, auth_key_version = $2
WHERE singleton;

-- name: UpsertSMTPChannel :one
INSERT INTO smtp_channel (
    singleton, enabled, host, port, from_address, recipient,
    auth_type, username, auth_ciphertext, auth_key_version, tls_mode, updated_at
)
VALUES (true, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (singleton) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    host = EXCLUDED.host,
    port = EXCLUDED.port,
    from_address = EXCLUDED.from_address,
    recipient = EXCLUDED.recipient,
    auth_type = EXCLUDED.auth_type,
    username = EXCLUDED.username,
    auth_ciphertext = EXCLUDED.auth_ciphertext,
    auth_key_version = EXCLUDED.auth_key_version,
    tls_mode = EXCLUDED.tls_mode,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: EnqueueAlertNotification :one
INSERT INTO notification_delivery (
    alert_instance_id, event_type, channel, target, template_id,
    payload, next_attempt_at, created_at
)
SELECT $1, $2, 'SMTP', smtp.recipient, $3, $4, $5, $5
FROM smtp_channel smtp
WHERE smtp.singleton AND smtp.enabled
RETURNING id;

-- name: EnqueueTestNotification :one
INSERT INTO notification_delivery (
    event_type, channel, target, template_id, payload, next_attempt_at, created_at
)
VALUES ('TEST', 'SMTP', $1, 'builtin.smtp.test.v1', '{}'::jsonb, $2, $2)
RETURNING id;

-- name: ClaimDueNotification :one
UPDATE notification_delivery delivery
SET locked_until = sqlc.arg(claimed_at)::timestamptz + interval '30 seconds'
WHERE delivery.id = (
    SELECT candidate.id
    FROM notification_delivery candidate
    WHERE candidate.status = 'PENDING'
      AND candidate.next_attempt_at <= sqlc.arg(claimed_at)
      AND (candidate.locked_until IS NULL OR candidate.locked_until <= sqlc.arg(claimed_at))
    ORDER BY candidate.next_attempt_at, candidate.created_at, candidate.id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- name: RecordNotificationSent :exec
WITH attempt AS (
    INSERT INTO notification_attempt (
        notification_id, attempted_at, result, retry_count
    ) VALUES ($1, $2, 'SENT', $3)
), completed AS (
    UPDATE notification_delivery
    SET status = 'SENT', attempt_count = $3 + 1,
        locked_until = NULL, completed_at = $2
    WHERE id = $1 AND status = 'PENDING'
    RETURNING alert_instance_id
)
INSERT INTO alert_event (
    alert_instance_id, rule_id, rule_version, kind,
    from_state, to_state, current_value, unavailability,
    rule_snapshot, evaluated_at
)
SELECT alert.id, alert.rule_id, alert.rule_version, 'NOTIFICATION_SENT',
       alert.status, alert.status, alert.current_value, alert.unavailability,
       alert.rule_snapshot, $2
FROM completed
JOIN alert_instance alert ON alert.id = completed.alert_instance_id;

-- name: RecordNotificationFailure :exec
WITH attempt AS (
    INSERT INTO notification_attempt (
        notification_id, attempted_at, result, failure_reason, retry_count
    ) VALUES ($1, $2, 'FAILED', $3, $4)
), completed AS (
    UPDATE notification_delivery
    SET status = CASE WHEN sqlc.arg(terminal)::boolean THEN 'FAILED' ELSE 'PENDING' END,
        attempt_count = $4 + 1,
        next_attempt_at = sqlc.arg(next_attempt_at),
        locked_until = NULL,
        completed_at = CASE WHEN sqlc.arg(terminal)::boolean THEN $2 ELSE NULL END
    WHERE id = $1 AND status = 'PENDING'
    RETURNING alert_instance_id
)
INSERT INTO alert_event (
    alert_instance_id, rule_id, rule_version, kind,
    from_state, to_state, current_value, unavailability,
    rule_snapshot, evaluated_at
)
SELECT alert.id, alert.rule_id, alert.rule_version, 'NOTIFICATION_FAILED',
       alert.status, alert.status, alert.current_value, alert.unavailability,
       alert.rule_snapshot, $2
FROM completed
JOIN alert_instance alert ON alert.id = completed.alert_instance_id
WHERE sqlc.arg(terminal)::boolean;

-- name: ListAlertNotificationAttempts :many
SELECT delivery.id, delivery.event_type, delivery.channel, delivery.target,
       delivery.template_id, delivery.status, delivery.attempt_count,
       delivery.created_at, delivery.completed_at,
       attempt.attempted_at, attempt.result, attempt.failure_reason, attempt.retry_count
FROM notification_delivery delivery
LEFT JOIN notification_attempt attempt ON attempt.notification_id = delivery.id
WHERE delivery.alert_instance_id = $1
ORDER BY delivery.created_at DESC, delivery.id, attempt.retry_count;
