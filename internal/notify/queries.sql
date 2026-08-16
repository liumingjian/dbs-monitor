-- name: GetSMTPChannel :one
SELECT * FROM smtp_channel WHERE singleton;

-- name: GetSMTPChannelForKeyRotation :one
SELECT * FROM smtp_channel WHERE singleton AND auth_key_version IS NOT NULL FOR UPDATE;

-- name: UpdateSMTPChannelAuthKey :exec
UPDATE smtp_channel
SET auth_ciphertext = $1, auth_key_version = $2
WHERE singleton;

-- name: ListNotificationContacts :many
SELECT * FROM notification_contact ORDER BY name, id;

-- name: GetNotificationContact :one
SELECT * FROM notification_contact WHERE id = $1;

-- name: CreateNotificationContact :one
INSERT INTO notification_contact (id, name, email, external_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $5)
RETURNING *;

-- name: UpdateNotificationContact :one
UPDATE notification_contact
SET name = $2, email = $3, external_id = $4, updated_at = $5
WHERE id = $1
RETURNING *;

-- name: DeleteNotificationContact :execrows
DELETE FROM notification_contact WHERE id = $1;

-- name: ListNotificationContactGroups :many
SELECT * FROM notification_contact_group ORDER BY name, id;

-- name: GetNotificationContactGroup :one
SELECT * FROM notification_contact_group WHERE id = $1;

-- name: CreateNotificationContactGroup :one
INSERT INTO notification_contact_group (id, name, created_at, updated_at)
VALUES ($1, $2, $3, $3)
RETURNING *;

-- name: UpdateNotificationContactGroup :one
UPDATE notification_contact_group
SET name = $2, updated_at = $3
WHERE id = $1
RETURNING *;

-- name: DeleteNotificationContactGroup :execrows
DELETE FROM notification_contact_group WHERE id = $1;

-- name: ListNotificationContactGroupMembers :many
SELECT contact_id FROM notification_contact_group_member
WHERE group_id = $1 ORDER BY contact_id;

-- name: ClearNotificationContactGroupMembers :exec
DELETE FROM notification_contact_group_member WHERE group_id = $1;

-- name: AddNotificationContactGroupMember :exec
INSERT INTO notification_contact_group_member (group_id, contact_id) VALUES ($1, $2);

-- name: ListNotificationPolicies :many
SELECT * FROM notification_policy ORDER BY is_default DESC, name, id;

-- name: GetNotificationPolicy :one
SELECT * FROM notification_policy WHERE id = $1;

-- name: CreateNotificationPolicy :one
INSERT INTO notification_policy (
    id, identifier, name, is_default, severity_filter, notify_on_fire,
    notify_on_recovery, repeat_interval, template_id, created_at, updated_at
)
VALUES ($1, $2, $3, false, $4, $5, $6, $7, $8, $9, $9)
RETURNING *;

-- name: UpdateNotificationPolicy :one
UPDATE notification_policy
SET name = $2, severity_filter = $3, notify_on_fire = $4,
    notify_on_recovery = $5, repeat_interval = $6, template_id = $7,
    updated_at = $8
WHERE id = $1
RETURNING *;

-- name: DeleteNotificationPolicy :execrows
DELETE FROM notification_policy WHERE id = $1 AND NOT is_default;

-- name: ListNotificationPolicyContacts :many
SELECT contact_id FROM notification_policy_contact WHERE policy_id = $1 ORDER BY contact_id;

-- name: ListNotificationPolicyContactGroups :many
SELECT group_id FROM notification_policy_contact_group WHERE policy_id = $1 ORDER BY group_id;

-- name: ListNotificationPolicyChannels :many
SELECT channel, channel_target_id FROM notification_policy_channel
WHERE policy_id = $1 ORDER BY channel, channel_target_id;

-- name: ClearNotificationPolicyContacts :exec
DELETE FROM notification_policy_contact WHERE policy_id = $1;

-- name: ClearNotificationPolicyContactGroups :exec
DELETE FROM notification_policy_contact_group WHERE policy_id = $1;

-- name: ClearNotificationPolicyChannels :exec
DELETE FROM notification_policy_channel WHERE policy_id = $1;

-- name: AddNotificationPolicyContact :exec
INSERT INTO notification_policy_contact (policy_id, contact_id) VALUES ($1, $2);

-- name: AddNotificationPolicyContactGroup :exec
INSERT INTO notification_policy_contact_group (policy_id, group_id) VALUES ($1, $2);

-- name: AddNotificationPolicyChannel :exec
INSERT INTO notification_policy_channel (policy_id, channel, channel_target_id) VALUES ($1, $2, $3);

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
    auth_type, username, auth_ciphertext, auth_key_version, tls_mode, updated_at, updated_by
)
VALUES (true, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
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
    updated_at = EXCLUDED.updated_at,
    updated_by = EXCLUDED.updated_by
RETURNING *;

-- name: ListMaintenanceWindows :many
SELECT * FROM maintenance_window
WHERE deleted_at IS NULL
ORDER BY starts_at DESC, id;

-- name: GetMaintenanceWindow :one
SELECT * FROM maintenance_window
WHERE id = $1 AND deleted_at IS NULL;

-- name: ListMaintenanceWindowInstances :many
SELECT instance_id FROM maintenance_window_instance
WHERE maintenance_window_id = $1
ORDER BY instance_id;

-- name: ListMaintenanceWindowsForInstance :many
SELECT maintenance.*
FROM maintenance_window maintenance
JOIN maintenance_window_instance scope ON scope.maintenance_window_id = maintenance.id
WHERE scope.instance_id = $1
  AND maintenance.deleted_at IS NULL
ORDER BY maintenance.ends_at, maintenance.id;

-- name: MaintenanceWindowInstanceExists :one
SELECT EXISTS (SELECT 1 FROM instance WHERE id = $1);

-- name: CreateMaintenanceWindow :one
INSERT INTO maintenance_window (
    id, starts_at, ends_at, reason, created_by, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING *;

-- name: UpdateMaintenanceWindow :one
UPDATE maintenance_window
SET starts_at = $2, ends_at = $3, reason = $4, updated_at = $5
WHERE id = $1 AND deleted_at IS NULL AND ends_at > $5
RETURNING *;

-- name: EndMaintenanceWindow :one
UPDATE maintenance_window
SET ends_at = $2, updated_at = $2
WHERE id = $1 AND deleted_at IS NULL AND starts_at <= $2 AND ends_at > $2
RETURNING *;

-- name: DeleteMaintenanceWindow :execrows
UPDATE maintenance_window
SET deleted_at = $2, updated_at = $2
WHERE id = $1 AND deleted_at IS NULL;

-- name: ClearMaintenanceWindowInstances :exec
DELETE FROM maintenance_window_instance WHERE maintenance_window_id = $1;

-- name: AddMaintenanceWindowInstance :exec
INSERT INTO maintenance_window_instance (maintenance_window_id, instance_id)
VALUES ($1, $2);

-- name: EnqueueAlertNotifications :many
WITH selected_policy AS (
    SELECT policy.*
    FROM alert_instance alert
    JOIN alert_rule rule ON rule.id = alert.rule_id
    JOIN notification_policy policy ON policy.id = COALESCE(
        rule.notification_policy_id,
        (SELECT id FROM notification_policy WHERE is_default)
    )
    WHERE alert.id = $1
      AND alert.severity = ANY(policy.severity_filter)
      AND (($2 = 'FIRING' AND policy.notify_on_fire)
        OR ($2 = 'RECOVERY' AND policy.notify_on_recovery)
        OR $2 = 'REPEAT')
), smtp_recipients AS (
    SELECT contact.email AS target
    FROM selected_policy policy
    JOIN notification_policy_channel channel
      ON channel.policy_id = policy.id AND channel.channel = 'SMTP'
    JOIN notification_policy_contact selected_contact ON selected_contact.policy_id = policy.id
    JOIN notification_contact contact ON contact.id = selected_contact.contact_id
    UNION
    SELECT contact.email
    FROM selected_policy policy
    JOIN notification_policy_channel channel
      ON channel.policy_id = policy.id AND channel.channel = 'SMTP'
    JOIN notification_policy_contact_group selected_group ON selected_group.policy_id = policy.id
    JOIN notification_contact_group_member member ON member.group_id = selected_group.group_id
    JOIN notification_contact contact ON contact.id = member.contact_id
)
INSERT INTO notification_delivery (
    alert_instance_id, event_type, channel, channel_target_id, target, template_id,
    payload, next_attempt_at, created_at
)
SELECT $1, $2, destination.channel, destination.channel_target_id,
       destination.target, $3, $4, $5, $5
FROM (
    SELECT 'SMTP'::text AS channel, NULL::uuid AS channel_target_id, recipient.target
    FROM smtp_channel smtp
    CROSS JOIN LATERAL (
        SELECT target FROM smtp_recipients
        UNION ALL
        SELECT smtp.recipient
        WHERE NOT EXISTS (SELECT 1 FROM smtp_recipients)
          AND EXISTS (
              SELECT 1 FROM selected_policy policy
              JOIN notification_policy_channel channel
                ON channel.policy_id = policy.id AND channel.channel = 'SMTP'
          )
    ) recipient
    WHERE smtp.singleton AND smtp.enabled
    UNION ALL
    SELECT 'WEBHOOK', webhook.id, webhook.url
    FROM selected_policy policy
    JOIN notification_policy_channel channel
      ON channel.policy_id = policy.id AND channel.channel = 'WEBHOOK'
    JOIN webhook_target webhook ON webhook.id = channel.channel_target_id
    WHERE webhook.enabled
) destination
RETURNING id;

-- name: DeletePendingNotification :exec
DELETE FROM notification_delivery WHERE id = $1 AND status = 'PENDING';

-- name: ListRepeatCandidates :many
SELECT alert.id AS alert_instance_id, alert.instance_id, alert.disposition,
       policy.repeat_interval,
       greatest(
           coalesce((SELECT max(delivery.created_at) FROM notification_delivery delivery
                     WHERE delivery.alert_instance_id = alert.id
                       AND delivery.event_type IN ('FIRING', 'REPEAT')), '-infinity'::timestamptz),
           coalesce((SELECT max(event.evaluated_at) FROM alert_event event
                     WHERE event.alert_instance_id = alert.id
                       AND event.kind = 'MAINTENANCE_SUPPRESSED'), '-infinity'::timestamptz),
           alert.first_triggered_at
       )::timestamptz AS last_notification_at,
       jsonb_build_object(
           'alert_instance_id', alert.id,
           'rule_name', coalesce(alert.rule_snapshot->>'name', rule.name),
           'instance_name', identity.name,
           'metric_id', alert.metric_id,
           'severity', alert.severity,
           'current_value', coalesce(alert.current_value::text, '')
       ) AS payload
FROM alert_instance alert
JOIN alert_rule rule ON rule.id = alert.rule_id
JOIN instance_identity identity ON identity.id = alert.instance_id
JOIN notification_policy policy ON policy.id = COALESCE(
    rule.notification_policy_id,
    (SELECT id FROM notification_policy WHERE is_default)
)
WHERE alert.status = 'FIRING'
  AND alert.first_triggered_at IS NOT NULL
  AND alert.severity = ANY(policy.severity_filter)
  AND (
      EXISTS (
          SELECT 1 FROM notification_delivery delivery
          WHERE delivery.alert_instance_id = alert.id
            AND delivery.event_type IN ('FIRING', 'REPEAT')
      )
      OR EXISTS (
          SELECT 1 FROM alert_event event
          WHERE event.alert_instance_id = alert.id
            AND event.kind = 'MAINTENANCE_SUPPRESSED'
      )
  )
  AND NOT EXISTS (
      SELECT 1 FROM notification_delivery pending
      WHERE pending.alert_instance_id = alert.id AND pending.status = 'PENDING'
  )
ORDER BY alert.id;

-- name: GetNotificationAlertInstance :one
SELECT alert.id, alert.instance_id
FROM notification_delivery delivery
JOIN alert_instance alert ON alert.id = delivery.alert_instance_id
WHERE delivery.id = $1;

-- name: RecordMaintenanceSuppressed :exec
WITH marked AS (
    UPDATE alert_instance alert
    SET in_maintenance = true, maintenance_window_id = $2
    WHERE alert.id = $1
    RETURNING alert.*
)
INSERT INTO alert_event (
    alert_instance_id, rule_id, rule_version, kind,
    from_state, to_state, current_value, unavailability,
    rule_snapshot, evaluated_at, in_maintenance, maintenance_window_id
)
SELECT alert.id, alert.rule_id, alert.rule_version, 'MAINTENANCE_SUPPRESSED',
       alert.status, alert.status, alert.current_value, alert.unavailability,
       alert.rule_snapshot, $3, true, $2
FROM marked alert;

-- name: SuppressNotificationForMaintenance :exec
WITH removed AS (
    DELETE FROM notification_delivery delivery
    WHERE delivery.id = $1 AND delivery.status = 'PENDING'
    RETURNING delivery.alert_instance_id
), marked AS (
    UPDATE alert_instance alert
    SET in_maintenance = true, maintenance_window_id = $2
    FROM removed
    WHERE alert.id = removed.alert_instance_id
    RETURNING alert.*
)
INSERT INTO alert_event (
    alert_instance_id, rule_id, rule_version, kind,
    from_state, to_state, current_value, unavailability,
    rule_snapshot, evaluated_at, in_maintenance, maintenance_window_id
)
SELECT alert.id, alert.rule_id, alert.rule_version, 'MAINTENANCE_SUPPRESSED',
       alert.status, alert.status, alert.current_value, alert.unavailability,
       alert.rule_snapshot, $3, true, $2
FROM marked alert;

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
