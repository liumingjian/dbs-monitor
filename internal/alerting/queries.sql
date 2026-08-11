-- name: ListAlertRules :many
SELECT * FROM alert_rule ORDER BY created_at, id;

-- name: GetAlertRule :one
SELECT * FROM alert_rule WHERE id = $1;

-- name: ListAlertRuleTemplates :many
SELECT * FROM alert_rule_template ORDER BY identifier;

-- name: GetAlertRuleTemplate :one
SELECT * FROM alert_rule_template WHERE identifier = $1;

-- name: GetDefaultNotificationPolicy :one
SELECT * FROM notification_policy WHERE is_default;

-- name: GetNotificationPolicy :one
SELECT * FROM notification_policy WHERE id = $1;

-- name: ListAlertRuleScopeInstances :many
SELECT instance_id
FROM alert_rule_scope_instance
WHERE rule_id = $1
ORDER BY instance_id;

-- name: AlertRuleTargetInstanceExists :one
SELECT EXISTS (SELECT 1 FROM instance WHERE id = $1);

-- name: CreateAlertRule :one
INSERT INTO alert_rule (
    id, name, metric_id, aggregation, operator, threshold,
    recovery_operator, recovery_threshold, window_seconds,
    consecutive_count, recovery_consecutive_count, severity,
    no_data_policy, scope, evaluation_interval_seconds,
    enabled, version, created_at, updated_at, notification_policy_id,
    source_template_id, source_template_version
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, 1, $17, $17, $18, $19, $20)
RETURNING *;

-- name: UpdateAlertRule :one
UPDATE alert_rule
SET name = $2,
    metric_id = $3,
    aggregation = $4,
    operator = $5,
    threshold = $6,
    recovery_operator = $7,
    recovery_threshold = $8,
    window_seconds = $9,
    consecutive_count = $10,
    recovery_consecutive_count = $11,
    severity = $12,
    no_data_policy = $13,
    scope = $14,
    evaluation_interval_seconds = $15,
    notification_policy_id = $16,
    version = version + 1,
    updated_at = $17
WHERE id = $1
RETURNING *;

-- name: SetAlertRuleEnabled :one
UPDATE alert_rule
SET enabled = $2,
    enabled_updated_by = $3,
    enabled_updated_at = $4
WHERE id = $1
RETURNING *;

-- name: DeleteAlertRule :one
DELETE FROM alert_rule
WHERE id = $1
  AND builtin_identifier IS NULL
RETURNING id;

-- name: DeleteAlertRuleScopeInstances :exec
DELETE FROM alert_rule_scope_instance WHERE rule_id = $1;

-- name: AddAlertRuleScopeInstance :exec
INSERT INTO alert_rule_scope_instance (rule_id, instance_id)
VALUES ($1, $2);

-- name: CreateAlertRuleVersion :exec
INSERT INTO alert_rule_version (rule_id, version, snapshot, created_at)
VALUES ($1, $2, $3, $4);

-- name: ListEvaluationTargets :many
SELECT rule.id AS rule_id,
       rule.name AS rule_name,
       instance.id AS instance_id,
       instance.name AS instance_name,
       COALESCE(metric_dimension.metric_dimension_key, '{}') AS metric_dimension_key
FROM alert_rule rule
CROSS JOIN instance
JOIN instance_collection_config collection_config
  ON collection_config.instance_id = instance.id
LEFT JOIN LATERAL (
    SELECT series.labels_key AS metric_dimension_key
    FROM metric_series series
    WHERE series.instance_id = instance.id
      AND series.metric_id = rule.metric_id
    UNION
    SELECT alert.metric_dimension_key
    FROM alert_instance alert
    WHERE alert.rule_id = rule.id
      AND alert.instance_id = instance.id
      AND alert.status <> 'RECOVERED'
) metric_dimension ON true
LEFT JOIN alert_rule_evaluation_state evaluation_state
  ON evaluation_state.rule_id = rule.id
 AND evaluation_state.instance_id = instance.id
 AND evaluation_state.metric_dimension_key = COALESCE(metric_dimension.metric_dimension_key, '{}')
WHERE rule.enabled
  AND NOT collection_config.collection_paused
  AND (rule.scope = 'ALL' OR EXISTS (
      SELECT 1
      FROM alert_rule_scope_instance scope_instance
      WHERE scope_instance.rule_id = rule.id
        AND scope_instance.instance_id = instance.id
  ))
  AND (evaluation_state.last_evaluated_at IS NULL
       OR evaluation_state.last_evaluated_at <= sqlc.arg(evaluated_at)::timestamptz - rule.evaluation_interval_seconds * interval '1 second')
ORDER BY instance.id, rule.id, COALESCE(metric_dimension.metric_dimension_key, '{}');

-- name: GetEvaluationTarget :one
SELECT rule.id AS rule_id,
       rule.metric_id,
       rule.aggregation,
       rule.operator,
       rule.threshold,
       rule.recovery_operator,
       rule.recovery_threshold,
       rule.window_seconds,
       rule.consecutive_count,
       rule.recovery_consecutive_count,
       rule.severity,
       rule.no_data_policy,
       rule.version,
       version.snapshot AS rule_snapshot,
       instance.id AS instance_id,
       instance.host,
       instance.port,
       instance.database_name,
       instance.username,
       instance.password_ciphertext,
       instance.password_key_version,
       instance.credential_version,
       collect_state.last_error_code,
       alert.id AS alert_instance_id,
       COALESCE(alert.status, 'OK') AS status,
       COALESCE(alert.rule_version, 0) AS evaluated_rule_version,
       COALESCE(alert.breach_count, 0) AS breach_count,
       COALESCE(alert.recovery_count, 0) AS recovery_count,
       COALESCE(alert.no_data_count, 0) AS no_data_count,
       alert.state_before_no_data,
       COALESCE(collection_config.agent_metrics_enabled, true) AS agent_metrics_enabled,
       instance.agent_expected
FROM alert_rule rule
JOIN alert_rule_version version
  ON version.rule_id = rule.id AND version.version = rule.version
CROSS JOIN instance
LEFT JOIN instance_collection_config collection_config
  ON collection_config.instance_id = instance.id
LEFT JOIN instance_collect_state collect_state
  ON collect_state.instance_id = instance.id AND collect_state.source = 'SERVER_DIRECT'
LEFT JOIN LATERAL (
    SELECT candidate.id,
           candidate.status,
           candidate.rule_version,
           candidate.breach_count,
           candidate.recovery_count,
           candidate.no_data_count,
           candidate.state_before_no_data
    FROM alert_instance candidate
    WHERE candidate.rule_id = rule.id
      AND candidate.instance_id = instance.id
      AND candidate.metric_dimension_key = sqlc.arg(metric_dimension_key)
    ORDER BY (candidate.status <> 'RECOVERED') DESC, candidate.updated_at DESC
    LIMIT 1
) alert ON true
WHERE rule.id = sqlc.arg(rule_id)
  AND instance.id = sqlc.arg(instance_id)
  AND rule.enabled;

-- name: SamplesInRuleWindow :many
SELECT sample.ts, sample.value
FROM metric_series series
JOIN metric_sample sample ON sample.series_id = series.series_id
WHERE series.instance_id = sqlc.arg(instance_id)
  AND series.metric_id = sqlc.arg(metric_id)
  AND series.labels_key = sqlc.arg(metric_dimension_key)
  AND sample.ts > sqlc.arg(window_start)
  AND sample.ts <= sqlc.arg(window_end)
ORDER BY sample.ts DESC;

-- name: SaveAlertSnapshot :one
INSERT INTO alert_instance (
    rule_id, instance_id, metric_id, metric_dimension_key,
    status, rule_version, severity, current_value, rule_snapshot,
    breach_count, recovery_count, no_data_count,
    state_before_no_data, unavailability, updated_at,
    first_triggered_at, first_rule_version, first_rule_snapshot, recovered_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
ON CONFLICT (rule_id, instance_id, metric_dimension_key)
WHERE status <> 'RECOVERED'
DO UPDATE SET metric_id = EXCLUDED.metric_id,
              status = EXCLUDED.status,
              rule_version = EXCLUDED.rule_version,
              severity = EXCLUDED.severity,
              current_value = EXCLUDED.current_value,
              rule_snapshot = EXCLUDED.rule_snapshot,
              breach_count = EXCLUDED.breach_count,
              recovery_count = EXCLUDED.recovery_count,
              no_data_count = EXCLUDED.no_data_count,
              state_before_no_data = EXCLUDED.state_before_no_data,
              unavailability = EXCLUDED.unavailability,
              updated_at = EXCLUDED.updated_at,
              first_triggered_at = COALESCE(alert_instance.first_triggered_at, EXCLUDED.first_triggered_at),
              first_rule_version = COALESCE(alert_instance.first_rule_version, EXCLUDED.first_rule_version),
              first_rule_snapshot = COALESCE(alert_instance.first_rule_snapshot, EXCLUDED.first_rule_snapshot),
              recovered_at = EXCLUDED.recovered_at
RETURNING id;

-- name: RecoverAlertSnapshot :one
UPDATE alert_instance
SET metric_id = $2,
    status = 'RECOVERED',
    rule_version = $3,
    severity = $4,
    current_value = $5,
    rule_snapshot = $6,
    breach_count = $7,
    recovery_count = $8,
    no_data_count = $9,
    state_before_no_data = NULL,
    unavailability = NULL,
    updated_at = $10,
    recovered_at = $10
WHERE id = $1
  AND status <> 'RECOVERED'
RETURNING id;

-- name: ResetIgnoredMissingAlert :exec
UPDATE alert_instance
SET rule_version = $2,
    severity = $3,
    rule_snapshot = $4,
    breach_count = $5,
    recovery_count = $6,
    no_data_count = $7,
    updated_at = $8
WHERE id = $1
  AND status <> 'RECOVERED';

-- name: MarkAlertRuleEvaluated :exec
INSERT INTO alert_rule_evaluation_state (
    rule_id, instance_id, metric_dimension_key, last_evaluated_at
)
VALUES ($1, $2, $3, $4)
ON CONFLICT (rule_id, instance_id, metric_dimension_key)
DO UPDATE SET last_evaluated_at = EXCLUDED.last_evaluated_at;

-- name: CreateAlertEvent :exec
INSERT INTO alert_event (
    alert_instance_id, rule_id, rule_version, kind,
    from_state, to_state, current_value, unavailability,
    rule_snapshot, evaluated_at, trigger_snapshot_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: CloseAlertsForInstanceRemoval :exec
WITH unresolved_alerts AS MATERIALIZED (
    SELECT id, rule_id, rule_version, status, current_value,
           unavailability, rule_snapshot
    FROM alert_instance
    WHERE alert_instance.instance_id = $1
      AND status <> 'RECOVERED'
    FOR UPDATE
), removal_events AS (
    INSERT INTO alert_event (
        alert_instance_id, rule_id, rule_version, kind,
        from_state, to_state, current_value, unavailability,
        rule_snapshot, evaluated_at, actor_id
    )
    SELECT id, rule_id, rule_version, 'INSTANCE_REMOVED',
           status, 'RECOVERED', current_value, unavailability,
           rule_snapshot, $2, $3
    FROM unresolved_alerts
    RETURNING alert_instance_id
)
UPDATE alert_instance
SET status = 'RECOVERED',
    state_before_no_data = NULL,
    unavailability = NULL,
    updated_at = $2,
    recovered_at = $2
WHERE id IN (SELECT alert_instance_id FROM removal_events);

-- name: CreateTriggerSnapshot :one
INSERT INTO alert_trigger_snapshot (
    alert_instance_id, captured_at, result,
    original_match_count, truncated, failure_reason
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: CreateTriggerSnapshotSession :exec
INSERT INTO alert_trigger_snapshot_session (
    snapshot_id, pid, username, database_name, client_address, state,
    query_started_at, transaction_started_at, query_duration_ms,
    transaction_duration_ms, wait_event_type, wait_event, blocking_pids
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- name: CreatePerformanceEvent :exec
INSERT INTO performance_event (alert_instance_id, event_type, derived_at)
VALUES ($1, $2, $3)
ON CONFLICT (alert_instance_id) DO NOTHING;

-- name: DeleteRecoveredAlertHistoryBefore :execrows
DELETE FROM alert_instance
WHERE recovered_at IS NOT NULL
  AND recovered_at <= $1;

-- name: GetAlertDispositionForRead :one
SELECT * FROM alert_instance WHERE id = $1 FOR SHARE;

-- name: GetAlertDispositionForUpdate :one
SELECT * FROM alert_instance WHERE id = $1 FOR UPDATE;

-- name: UpdateAlertDisposition :one
UPDATE alert_instance
SET disposition = $2,
    disposition_by = $3,
    disposition_at = $4,
    disposition_note = $5,
    ignore_reason_code = $6,
    ignore_reason_detail = $7
WHERE id = $1
RETURNING *;

-- name: CreateAlertDispositionEvent :exec
INSERT INTO alert_event (
    alert_instance_id, rule_id, rule_version, kind,
    from_state, to_state, current_value, unavailability,
    rule_snapshot, evaluated_at, actor_id, acted_at,
    from_disposition, to_disposition, disposition_note,
    ignore_reason_code, ignore_reason_detail
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17);

-- name: ListAlertDispositionEvents :many
SELECT *
FROM alert_event
WHERE alert_instance_id = $1
  AND kind IN ('ACKED', 'IGNORED')
ORDER BY acted_at, id;

-- name: GetAlertStatus :one
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

-- name: ListInstanceHealthAlerts :many
SELECT alert.instance_id, alert.status, alert.severity, alert.current_value,
       COALESCE(alert.first_triggered_at, alert.updated_at) AS first_triggered_at,
       alert.recovered_at, alert.disposition, rule.name AS rule_name
FROM alert_instance alert
JOIN alert_rule rule ON rule.id = alert.rule_id
WHERE alert.status <> 'RECOVERED' OR alert.recovered_at >= $1
ORDER BY alert.instance_id, first_triggered_at, alert.id;
