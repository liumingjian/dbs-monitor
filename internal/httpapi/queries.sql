-- name: AdminExists :one
SELECT EXISTS (SELECT 1 FROM app_user WHERE enabled AND role = 'PLATFORM_ADMIN');

-- name: CreateAdmin :exec
INSERT INTO app_user (id, username, password_hash, role)
VALUES ($1, $2, $3, 'PLATFORM_ADMIN')
ON CONFLICT (username) DO NOTHING;

-- name: GetUserForLogin :one
SELECT id, password_hash, role FROM app_user WHERE username = $1 AND enabled;

-- name: GetUserPassword :one
SELECT password_hash FROM app_user WHERE id = $1 AND enabled;

-- name: CreateSession :exec
INSERT INTO user_session (token_hash, user_id, expires_at)
VALUES ($1, $2, $3);

-- name: GetSessionUser :one
SELECT u.id, u.role
FROM user_session session
JOIN app_user u ON u.id = session.user_id
WHERE session.token_hash = $1 AND session.expires_at > $2 AND u.enabled;

-- name: GetCurrentUser :one
SELECT id, username, role, enabled, created_at
FROM app_user
WHERE id = $1;

-- name: ListUsers :many
SELECT id, username, role, enabled, created_at
FROM app_user
ORDER BY username;

-- name: CreateUser :one
INSERT INTO app_user (id, username, password_hash, role)
VALUES ($1, $2, $3, $4)
RETURNING id, username, role, enabled, created_at;

-- name: LockEnabledPlatformAdmins :many
SELECT id
FROM app_user
WHERE enabled AND role = 'PLATFORM_ADMIN'
ORDER BY id
FOR UPDATE;

-- name: GetUserForUpdate :one
SELECT id, username, role, enabled, created_at
FROM app_user
WHERE id = $1
FOR UPDATE;

-- name: SetUserEnabled :one
UPDATE app_user
SET enabled = $2
WHERE id = $1
RETURNING id, username, role, enabled, created_at;

-- name: SetUserRole :one
UPDATE app_user
SET role = $2
WHERE id = $1
RETURNING id, username, role, enabled, created_at;

-- name: SetUserPassword :execrows
UPDATE app_user
SET password_hash = $2
WHERE id = $1;

-- name: DeleteUserSessions :exec
DELETE FROM user_session WHERE user_id = $1;

-- name: GetAgentTokenHash :one
SELECT agent_token_hash
FROM instance
WHERE id = $1
  AND agent_expected
  AND agent_token_hash IS NOT NULL
  AND agent_token_revoked_at IS NULL;

-- name: GetCollectionPause :one
SELECT collection_paused, collection_pause_updated_by,
       collection_pause_updated_at, collection_pause_reason
FROM instance_collection_config
WHERE instance_id = $1;

-- name: SetCollectionPause :one
UPDATE instance_collection_config
SET collection_paused = $2,
    collection_pause_updated_by = $3,
    collection_pause_updated_at = $4,
    collection_pause_reason = $5,
    updated_at = $4
WHERE instance_id = $1
  AND collection_paused <> $2
RETURNING collection_paused, collection_pause_updated_by,
          collection_pause_updated_at, collection_pause_reason;

-- name: CreateCollectionPauseEvents :exec
INSERT INTO alert_event (
    alert_instance_id, rule_id, rule_version, kind,
    from_state, to_state, current_value, unavailability,
    rule_snapshot, evaluated_at, actor_id
)
SELECT alert.id, alert.rule_id, alert.rule_version, $2,
       alert.status, alert.status, alert.current_value, alert.unavailability,
       alert.rule_snapshot, $3, $4
FROM alert_instance alert
WHERE alert.instance_id = $1
  AND alert.status <> 'RECOVERED';

-- name: GetInstanceAlertStatus :one
SELECT status
FROM alert_instance
WHERE instance_id = $1
ORDER BY CASE status
    WHEN 'FIRING' THEN 5
    WHEN 'NO_DATA' THEN 4
    WHEN 'PENDING' THEN 3
    WHEN 'RECOVERED' THEN 2
    ELSE 1
END DESC
LIMIT 1;

-- name: GetAlertInstanceMetricID :one
SELECT metric_id
FROM alert_instance
WHERE id = $1;

-- name: GetAlertTriggerSnapshot :one
SELECT id, captured_at, result, original_match_count, truncated, failure_reason
FROM alert_trigger_snapshot
WHERE alert_instance_id = $1;

-- name: ListAlertTriggerSnapshotSessions :many
SELECT pid, username, database_name, client_address, state,
       query_started_at, transaction_started_at,
       query_duration_ms, transaction_duration_ms,
       wait_event_type, wait_event, blocking_pids
FROM alert_trigger_snapshot_session
WHERE snapshot_id = $1
ORDER BY pid;

-- name: ListPersistedCollectionTaskStates :many
SELECT task.task_id, task.last_due_at, task.last_started_at, task.last_finished_at, task.last_success_at,
       task.last_result, task.consecutive_failures,
       CASE
           WHEN task.next_eligible_at IS NULL THEN connection.next_eligible_at
           WHEN connection.next_eligible_at IS NULL THEN task.next_eligible_at
           ELSE GREATEST(task.next_eligible_at, connection.next_eligible_at)
       END::timestamptz AS next_eligible_at,
       task.last_error_code, task.last_error_message
FROM instance_collection_task_state task
LEFT JOIN instance_collection_connection_state connection ON connection.instance_id = task.instance_id
WHERE task.instance_id = $1
ORDER BY task.task_id;

-- name: CountLongQuerySamples :one
SELECT count(*)
FROM long_query_sample
WHERE instance_id = sqlc.arg(instance_id)
  AND sampled_at >= sqlc.arg(from_time)
  AND sampled_at <= sqlc.arg(to_time);

-- name: ListLongQuerySamples :many
SELECT sample.sampled_at, sample.pid, sample.username, sample.database_name,
       sample.client_address, sample.state, sample.query_started_at,
       sample.transaction_started_at, sample.query_duration_ms,
       sample.transaction_duration_ms, sample.wait_event_type, sample.wait_event,
       sample.blocking_pids, snapshot.original_count, snapshot.truncated
FROM long_query_sample sample
JOIN long_query_sample_snapshot snapshot
  ON snapshot.instance_id = sample.instance_id AND snapshot.sampled_at = sample.sampled_at
WHERE sample.instance_id = sqlc.arg(instance_id)
  AND sample.sampled_at >= sqlc.arg(from_time)
  AND sample.sampled_at <= sqlc.arg(to_time)
ORDER BY
  CASE WHEN sqlc.arg(sort_order)::text = 'sampled_at' THEN sample.sampled_at END ASC,
  CASE WHEN sqlc.arg(sort_order)::text = '-sampled_at' THEN sample.sampled_at END DESC,
  CASE WHEN sqlc.arg(sort_order)::text = 'query_started_at' THEN sample.query_started_at END ASC,
  CASE WHEN sqlc.arg(sort_order)::text = '-query_started_at' THEN sample.query_started_at END DESC,
  sample.sampled_at DESC, sample.pid
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);
