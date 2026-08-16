-- name: AdminExists :one
SELECT EXISTS (SELECT 1 FROM app_user WHERE enabled AND role = 'PLATFORM_ADMIN');

-- name: CreateAdmin :exec
INSERT INTO app_user (id, username, password_hash, role)
VALUES ($1, $2, $3, 'PLATFORM_ADMIN')
ON CONFLICT (username) DO NOTHING;

-- name: GetUserForLogin :one
SELECT id, password_hash, role
FROM app_user
WHERE username = $1 AND enabled
FOR SHARE;

-- name: GetUserPassword :one
SELECT password_hash
FROM app_user
WHERE id = $1 AND enabled
FOR UPDATE;

-- name: CreateSession :exec
INSERT INTO user_session (token_hash, user_id, expires_at, created_at, last_seen_at)
VALUES ($1, $2, $3, $4, $4);

-- name: AuthenticateSession :one
UPDATE user_session AS session
SET last_seen_at = sqlc.arg(now_time)
FROM app_user AS u
WHERE session.user_id = u.id
  AND session.token_hash = sqlc.arg(token_hash)
  AND session.expires_at > sqlc.arg(now_time)
  AND session.last_seen_at > sqlc.arg(idle_cutoff)
  AND u.enabled
RETURNING u.id, u.role;

-- name: DeleteSession :exec
DELETE FROM user_session
WHERE token_hash = $1;

-- name: GetCurrentUser :one
SELECT id, username, role, enabled, created_at
FROM app_user
WHERE id = $1;

-- name: ListUsers :many
SELECT id, username, role, enabled, created_at
FROM app_user
ORDER BY username;

-- name: ListPlatformEvents :many
SELECT event.id, event.kind, event.occurred_at,
       coalesce(actor.username, event.actor_subject)::text AS actor,
       event.subject_id
FROM platform_event event
LEFT JOIN app_user actor ON actor.id = event.actor_id
ORDER BY event.occurred_at DESC, event.id DESC
LIMIT 100;

-- name: CreateUser :one
INSERT INTO app_user (id, username, password_hash, role, created_by)
VALUES ($1, $2, $3, $4, $5)
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
SET enabled = $2,
    enabled_updated_by = $3,
    enabled_updated_at = $4
WHERE id = $1
RETURNING id, username, role, enabled, created_at;

-- name: SetUserRole :one
UPDATE app_user
SET role = $2,
    role_updated_by = $3,
    role_updated_at = $4
WHERE id = $1
RETURNING id, username, role, enabled, created_at;

-- name: SetUserPassword :execrows
UPDATE app_user
SET password_hash = $2
WHERE id = $1;

-- name: DeleteUserSessions :exec
DELETE FROM user_session WHERE user_id = $1;

-- name: DeleteOtherUserSessions :exec
DELETE FROM user_session
WHERE user_id = $1
  AND token_hash <> $2;

-- name: GetAgentTokenHash :one
SELECT agent_token_hash
FROM instance
WHERE id = $1
  AND agent_expected
  AND agent_token_hash IS NOT NULL
  AND agent_token_revoked_at IS NULL;

-- name: GetInstanceIDByAgentTokenHash :one
SELECT id
FROM instance
WHERE agent_token_hash = $1
  AND agent_expected
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

-- name: GetAlertInstanceMetricID :one
SELECT metric_id
FROM alert_instance
WHERE id = $1;

-- name: CountAlertObservations :one
SELECT count(*)
FROM alert_instance alert
LEFT JOIN instance_collection_config config ON config.instance_id = alert.instance_id
WHERE (sqlc.arg(recovered)::boolean = (alert.status = 'RECOVERED'))
  AND (NOT sqlc.arg(has_instance)::boolean OR alert.instance_id = sqlc.arg(instance_id))
  AND (sqlc.arg(include_paused)::boolean OR NOT coalesce(config.collection_paused, false));

-- name: ListAlertObservations :many
SELECT alert.id, alert.instance_id, identity.name AS instance_name,
       alert.rule_id, coalesce(alert.rule_snapshot->>'name', rule.name) AS rule_name,
       alert.rule_version, alert.rule_snapshot, alert.metric_id, alert.status,
       alert.severity, alert.disposition,
       alert.in_maintenance, alert.maintenance_window_id,
       coalesce(config.collection_paused, false) AS paused,
       config.collection_pause_updated_at AS paused_at,
       coalesce((SELECT event.current_value FROM alert_event event
                 WHERE event.alert_instance_id = alert.id AND event.kind = 'FIRED'
                 ORDER BY event.evaluated_at, event.id LIMIT 1), alert.current_value) AS current_value,
       coalesce((alert.first_rule_snapshot->>'threshold')::double precision,
                (alert.rule_snapshot->>'threshold')::double precision)::double precision AS threshold,
       alert.first_triggered_at, alert.updated_at, alert.recovered_at,
       coalesce(alert.unavailability, (SELECT event.unavailability FROM alert_event event
                 WHERE event.alert_instance_id = alert.id AND event.unavailability IS NOT NULL
                 ORDER BY event.evaluated_at DESC, event.id DESC LIMIT 1)) AS unavailability
FROM alert_instance alert
JOIN instance_identity identity ON identity.id = alert.instance_id
JOIN alert_rule rule ON rule.id = alert.rule_id
LEFT JOIN instance_collection_config config ON config.instance_id = alert.instance_id
WHERE (sqlc.arg(recovered)::boolean = (alert.status = 'RECOVERED'))
  AND (NOT sqlc.arg(has_instance)::boolean OR alert.instance_id = sqlc.arg(instance_id))
  AND (sqlc.arg(include_paused)::boolean OR NOT coalesce(config.collection_paused, false))
ORDER BY coalesce(alert.first_triggered_at, alert.updated_at) DESC, alert.id
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: GetAlertObservation :one
SELECT alert.id, alert.instance_id, identity.name AS instance_name,
       alert.rule_id, coalesce(alert.rule_snapshot->>'name', rule.name) AS rule_name,
       alert.rule_version, alert.rule_snapshot, alert.metric_id, alert.status,
       alert.severity, alert.disposition,
       alert.in_maintenance, alert.maintenance_window_id,
       coalesce(config.collection_paused, false) AS paused,
       config.collection_pause_updated_at AS paused_at,
       coalesce((SELECT event.current_value FROM alert_event event
                 WHERE event.alert_instance_id = alert.id AND event.kind = 'FIRED'
                 ORDER BY event.evaluated_at, event.id LIMIT 1), alert.current_value) AS current_value,
       coalesce((alert.first_rule_snapshot->>'threshold')::double precision,
                (alert.rule_snapshot->>'threshold')::double precision)::double precision AS threshold,
       alert.first_triggered_at, alert.updated_at, alert.recovered_at,
       coalesce(alert.unavailability, (SELECT event.unavailability FROM alert_event event
                 WHERE event.alert_instance_id = alert.id AND event.unavailability IS NOT NULL
                 ORDER BY event.evaluated_at DESC, event.id DESC LIMIT 1)) AS unavailability
FROM alert_instance alert
JOIN instance_identity identity ON identity.id = alert.instance_id
JOIN alert_rule rule ON rule.id = alert.rule_id
LEFT JOIN instance_collection_config config ON config.instance_id = alert.instance_id
WHERE alert.id = $1;

-- name: ListAlertRuleVersionHistory :many
SELECT DISTINCT ON (rule_version) rule_version, rule_snapshot, evaluated_at
FROM alert_event
WHERE alert_instance_id = $1
  AND kind NOT IN ('ACKED', 'IGNORED')
ORDER BY rule_version, evaluated_at;

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

-- name: CountPerformanceEvents :one
SELECT count(*)
FROM performance_event event
JOIN alert_instance alert ON alert.id = event.alert_instance_id
WHERE alert.instance_id = sqlc.arg(instance_id)
  AND event.derived_at >= sqlc.arg(from_time)
  AND event.derived_at <= sqlc.arg(to_time)
  AND (sqlc.narg(recovered)::boolean IS NULL
       OR (alert.recovered_at IS NOT NULL) = sqlc.narg(recovered)::boolean)
  AND (sqlc.narg(disposition)::text IS NULL
       OR alert.disposition = sqlc.narg(disposition)::text);

-- name: ListPerformanceEvents :many
SELECT event.id, event.alert_instance_id, event.event_type, event.derived_at,
       alert.instance_id, alert.status AS alert_status, alert.severity,
       alert.disposition, alert.updated_at, alert.recovered_at, alert.metric_id,
       fired.current_value AS trigger_value, fired.in_maintenance,
       fired.maintenance_window_id,
       (fired.rule_snapshot ->> 'threshold')::double precision AS threshold,
       snapshot.result AS trigger_snapshot_result
FROM performance_event event
JOIN alert_instance alert ON alert.id = event.alert_instance_id
JOIN LATERAL (
    SELECT current_value, rule_snapshot, in_maintenance, maintenance_window_id
    FROM alert_event
    WHERE alert_instance_id = alert.id AND kind = 'FIRED'
    ORDER BY evaluated_at, id
    LIMIT 1
) fired ON true
LEFT JOIN alert_trigger_snapshot snapshot ON snapshot.alert_instance_id = alert.id
WHERE alert.instance_id = sqlc.arg(instance_id)
  AND event.derived_at >= sqlc.arg(from_time)
  AND event.derived_at <= sqlc.arg(to_time)
  AND (sqlc.narg(recovered)::boolean IS NULL
       OR (alert.recovered_at IS NOT NULL) = sqlc.narg(recovered)::boolean)
  AND (sqlc.narg(disposition)::text IS NULL
       OR alert.disposition = sqlc.narg(disposition)::text)
ORDER BY
  CASE WHEN sqlc.arg(sort_order)::text = 'derived_at' THEN event.derived_at END ASC,
  CASE WHEN sqlc.arg(sort_order)::text = '-derived_at' THEN event.derived_at END DESC,
  CASE WHEN sqlc.arg(sort_order)::text = 'updated_at' THEN alert.updated_at END ASC,
  CASE WHEN sqlc.arg(sort_order)::text = '-updated_at' THEN alert.updated_at END DESC,
  event.derived_at DESC, event.id
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: GetLatestQueryStatisticsSnapshot :one
SELECT sampled_at
FROM query_statistics_snapshot
WHERE instance_id = sqlc.arg(instance_id)
  AND sampled_at >= sqlc.arg(lower_bound)
ORDER BY sampled_at DESC
LIMIT 1;

-- name: HasQueryStatisticsSnapshot :one
SELECT EXISTS (
    SELECT 1 FROM query_statistics_snapshot WHERE instance_id = $1
);

-- name: ListQueryStatisticsSnapshotEntries :many
SELECT queryid, database_oid, user_oid, calls, total_exec_time_ms
FROM query_statistics_snapshot_entry
WHERE instance_id = sqlc.arg(instance_id)
  AND sampled_at = sqlc.arg(sampled_at)
ORDER BY total_exec_time_ms DESC, queryid, database_oid, user_oid;

-- name: GetRecentSessionSnapshot :one
SELECT sampled_at, original_count, truncated
FROM instance_session_snapshot
WHERE instance_id = sqlc.arg(instance_id)
  AND sampled_at >= sqlc.arg(lower_bound)
LIMIT 1;

-- name: HasSessionSnapshot :one
SELECT EXISTS (
    SELECT 1 FROM instance_session_snapshot WHERE instance_id = $1
);

-- name: ListSessionSnapshotEntries :many
SELECT pid, username, database_name, client_address, state,
       query_started_at, transaction_started_at, query_duration_ms,
       transaction_duration_ms, wait_event_type, wait_event, blocking_pids
FROM instance_session_snapshot_entry
WHERE instance_id = sqlc.arg(instance_id)
ORDER BY pid
LIMIT sqlc.arg(row_limit);

-- name: GetPerformanceEvent :one
SELECT event.id, event.alert_instance_id, event.event_type, event.derived_at,
       alert.instance_id, alert.status AS alert_status, alert.severity,
       alert.disposition, alert.updated_at, alert.recovered_at, alert.metric_id,
       fired.current_value AS trigger_value, fired.in_maintenance,
       fired.maintenance_window_id,
       (fired.rule_snapshot ->> 'threshold')::double precision AS threshold,
       snapshot.result AS trigger_snapshot_result
FROM performance_event event
JOIN alert_instance alert ON alert.id = event.alert_instance_id
JOIN LATERAL (
    SELECT current_value, rule_snapshot, in_maintenance, maintenance_window_id
    FROM alert_event
    WHERE alert_instance_id = alert.id AND kind = 'FIRED'
    ORDER BY evaluated_at, id
    LIMIT 1
) fired ON true
LEFT JOIN alert_trigger_snapshot snapshot ON snapshot.alert_instance_id = alert.id
WHERE event.id = $1;

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
