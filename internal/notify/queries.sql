-- name: GetSMTPChannel :one
SELECT * FROM smtp_channel WHERE singleton;

-- name: GetSMTPChannelForKeyRotation :one
SELECT * FROM smtp_channel WHERE singleton AND auth_key_version IS NOT NULL FOR UPDATE;

-- name: UpdateSMTPChannelAuthKey :exec
UPDATE smtp_channel
SET auth_ciphertext = $1, auth_key_version = $2
WHERE singleton;

-- name: ListWebhookTargets :many
SELECT * FROM webhook_target ORDER BY name, id;

-- name: GetWebhookTarget :one
SELECT * FROM webhook_target WHERE id = $1;

-- name: ListWebhookTargetsForKeyRotation :many
SELECT * FROM webhook_target ORDER BY id FOR UPDATE;

-- name: CreateWebhookTarget :one
INSERT INTO webhook_target (
    id, name, enabled, url, signing_value_ciphertext,
    signature_header_ciphertext, signing_key_version, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
RETURNING *;

-- name: UpdateWebhookTarget :one
UPDATE webhook_target
SET name = $2,
    enabled = $3,
    url = $4,
    signing_value_ciphertext = $5,
    signature_header_ciphertext = $6,
    signing_key_version = $7,
    updated_at = $8
WHERE id = $1
RETURNING *;

-- name: UpdateWebhookTargetSigningKey :exec
UPDATE webhook_target
SET signing_value_ciphertext = $2,
    signature_header_ciphertext = $3,
    signing_key_version = $4
WHERE id = $1;

-- name: DeleteWebhookTarget :execrows
DELETE FROM webhook_target WHERE id = $1;

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

-- name: EnqueueAlertNotifications :many
INSERT INTO notification_delivery (
    alert_instance_id, event_type, channel, channel_target_id, target, template_id,
    payload, next_attempt_at, created_at
)
SELECT $1, $2, destination.channel, destination.channel_target_id,
       destination.target, $3, $4, $5, $5
FROM (
    SELECT 'SMTP'::text AS channel, NULL::uuid AS channel_target_id, smtp.recipient AS target
    FROM smtp_channel smtp
    WHERE smtp.singleton AND smtp.enabled
    UNION ALL
    SELECT 'WEBHOOK', webhook.id, webhook.url
    FROM webhook_target webhook
    WHERE webhook.enabled
) destination
RETURNING id;

-- name: EnqueueTestNotification :one
INSERT INTO notification_delivery (
    event_type, channel, target, template_id, payload, next_attempt_at, created_at
)
VALUES ('TEST', 'SMTP', $1, 'builtin.smtp.test.v1', '{}'::jsonb, $2, $2)
RETURNING id;

-- name: EnqueueTestWebhookNotification :one
INSERT INTO notification_delivery (
    event_type, channel, channel_target_id, target, template_id, payload, next_attempt_at, created_at
)
SELECT 'TEST', 'WEBHOOK', webhook.id, webhook.url,
       'builtin.webhook.test.v1', '{}'::jsonb, $2, $2
FROM webhook_target webhook
WHERE webhook.id = $1 AND webhook.enabled
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

-- name: ListActiveChannelFailureSummaries :many
WITH terminal_failures AS (
    SELECT delivery.channel, delivery.channel_target_id, delivery.target,
           delivery.completed_at AS failed_at, attempt.failure_reason,
           delivery.attempt_count - 1 AS retry_count
    FROM notification_delivery delivery
    JOIN notification_attempt attempt
      ON attempt.notification_id = delivery.id
     AND attempt.retry_count = delivery.attempt_count - 1
    WHERE delivery.status = 'FAILED'
      AND (delivery.channel <> 'WEBHOOK' OR EXISTS (
          SELECT 1 FROM webhook_target webhook WHERE webhook.id = delivery.channel_target_id
      ))
      AND NOT EXISTS (
          SELECT 1
          FROM notification_delivery sent
          WHERE sent.status = 'SENT'
            AND sent.channel = delivery.channel
            AND sent.channel_target_id IS NOT DISTINCT FROM delivery.channel_target_id
            AND sent.completed_at > delivery.completed_at
      )
)
SELECT channel, channel_target_id,
       ((array_agg(target ORDER BY failed_at DESC))[1])::text AS target,
       count(*)::integer AS recent_failure_count,
       ((array_agg(failure_reason ORDER BY failed_at DESC))[1])::text AS last_failure_reason,
       max(failed_at)::timestamptz AS last_failed_at
FROM terminal_failures
GROUP BY channel, channel_target_id
ORDER BY channel, channel_target_id;

-- name: ListRecentActiveChannelFailures :many
WITH terminal_failures AS (
    SELECT delivery.channel, delivery.channel_target_id, delivery.target,
           delivery.completed_at AS failed_at, attempt.failure_reason,
           delivery.attempt_count - 1 AS retry_count,
           row_number() OVER (
               PARTITION BY delivery.channel, delivery.channel_target_id
               ORDER BY delivery.completed_at DESC, delivery.id
           ) AS failure_number
    FROM notification_delivery delivery
    JOIN notification_attempt attempt
      ON attempt.notification_id = delivery.id
     AND attempt.retry_count = delivery.attempt_count - 1
    WHERE delivery.status = 'FAILED'
      AND (delivery.channel <> 'WEBHOOK' OR EXISTS (
          SELECT 1 FROM webhook_target webhook WHERE webhook.id = delivery.channel_target_id
      ))
      AND NOT EXISTS (
          SELECT 1
          FROM notification_delivery sent
          WHERE sent.status = 'SENT'
            AND sent.channel = delivery.channel
            AND sent.channel_target_id IS NOT DISTINCT FROM delivery.channel_target_id
            AND sent.completed_at > delivery.completed_at
      )
)
SELECT channel, channel_target_id, failed_at, target, failure_reason, retry_count
FROM terminal_failures
WHERE failure_number <= 20
ORDER BY channel, channel_target_id, failed_at DESC;
