-- name: ListAlertRules :many
SELECT * FROM alert_rule ORDER BY created_at, id;

-- name: CreateAlertRule :one
INSERT INTO alert_rule (
    id, name, metric_id, aggregation, operator, threshold,
    recovery_operator, recovery_threshold, window_seconds,
    consecutive_count, recovery_consecutive_count, severity,
    no_data_policy, enabled, version, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 1, $15, $15)
RETURNING *;

-- name: CreateAlertRuleVersion :exec
INSERT INTO alert_rule_version (rule_id, version, snapshot, created_at)
VALUES ($1, $2, $3, $4);

-- name: ListEvaluationTargets :many
SELECT rule.id AS rule_id, instance.id AS instance_id
FROM alert_rule rule
CROSS JOIN instance
WHERE rule.enabled
ORDER BY instance.id, rule.id;

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
       collect_state.last_error_code,
       alert.id AS alert_instance_id,
       alert.status,
       alert.rule_version AS evaluated_rule_version,
       alert.breach_count,
       alert.recovery_count,
       alert.no_data_count,
       alert.state_before_no_data
FROM alert_rule rule
JOIN alert_rule_version version
  ON version.rule_id = rule.id AND version.version = rule.version
CROSS JOIN instance
LEFT JOIN instance_collect_state collect_state
  ON collect_state.instance_id = instance.id AND collect_state.source = 'SERVER_DIRECT'
LEFT JOIN alert_instance alert
  ON alert.rule_id = rule.id
 AND alert.instance_id = instance.id
 AND alert.metric_dimension_key = '{}'
WHERE rule.id = sqlc.arg(rule_id)
  AND instance.id = sqlc.arg(instance_id);

-- name: SamplesInRuleWindow :many
SELECT sample.ts, sample.value
FROM metric_series series
JOIN metric_sample sample ON sample.series_id = series.series_id
WHERE series.instance_id = $1
  AND series.metric_id = $2
  AND sample.ts > $3
  AND sample.ts <= $4
ORDER BY sample.ts DESC;

-- name: SaveAlertSnapshot :one
INSERT INTO alert_instance (
    rule_id, instance_id, metric_id, metric_dimension_key,
    status, rule_version, severity, current_value, rule_snapshot,
    breach_count, recovery_count, no_data_count,
    state_before_no_data, unavailability, updated_at
)
VALUES ($1, $2, $3, '{}', $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (rule_id, instance_id, metric_dimension_key)
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
              updated_at = EXCLUDED.updated_at
RETURNING id;

-- name: CreateAlertEvent :exec
INSERT INTO alert_event (
    alert_instance_id, rule_id, rule_version, kind,
    from_state, to_state, current_value, unavailability,
    rule_snapshot, evaluated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

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
