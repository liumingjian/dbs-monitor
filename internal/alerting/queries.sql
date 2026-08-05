-- name: ListEvaluationTargetIDs :many
SELECT id FROM instance ORDER BY id;

-- name: GetEvaluationTarget :one
SELECT i.id,
       s.last_success_at,
       s.last_error_code,
       a.status,
       a.breach_count,
       a.recovery_count,
       a.no_data_count,
       a.state_before_no_data
FROM instance i
LEFT JOIN instance_collect_state s
  ON s.instance_id = i.id AND s.source = 'SERVER_DIRECT'
LEFT JOIN alert_instance a ON a.instance_id = i.id
WHERE i.id = $1;

-- name: LatestConnectionPoint :one
SELECT ms.ts, ms.value
FROM metric_series series
JOIN metric_sample ms ON ms.series_id = series.series_id
WHERE series.instance_id = $1
  AND series.metric_id = 'pg.connection.total'
  AND ms.ts > $2
ORDER BY ms.ts DESC
LIMIT 1;

-- name: SaveAlertSnapshot :exec
INSERT INTO alert_instance (
    instance_id, metric_id, status, breach_count, recovery_count,
    no_data_count, state_before_no_data, unavailability, updated_at
)
VALUES ($1, 'pg.connection.total', $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (instance_id)
DO UPDATE SET status = EXCLUDED.status,
              breach_count = EXCLUDED.breach_count,
              recovery_count = EXCLUDED.recovery_count,
              no_data_count = EXCLUDED.no_data_count,
              state_before_no_data = EXCLUDED.state_before_no_data,
              unavailability = EXCLUDED.unavailability,
              updated_at = EXCLUDED.updated_at;

-- name: GetAlertStatus :one
SELECT status FROM alert_instance WHERE instance_id = $1;
