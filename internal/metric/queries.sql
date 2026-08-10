-- name: UpsertSeries :one
INSERT INTO metric_series (instance_id, metric_id, labels, labels_key, last_seen)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (instance_id, metric_id, labels_key)
DO UPDATE SET last_seen = EXCLUDED.last_seen
RETURNING series_id;

-- name: SeriesForMetric :many
SELECT series_id, labels
FROM metric_series
WHERE instance_id = $1 AND metric_id = $2
ORDER BY series_id;

-- name: PointsInRange :many
SELECT ts, value
FROM metric_sample
WHERE series_id = $1 AND ts >= $2 AND ts <= $3
ORDER BY ts;

-- name: BucketedPointsInRange :many
SELECT date_bin(sqlc.arg(bucket)::interval, ts, '2000-01-01'::timestamptz)::timestamptz AS ts,
       avg(value)::double precision AS value
FROM metric_sample
WHERE series_id = $1 AND ts >= $2 AND ts <= $3
GROUP BY 1
ORDER BY 1;

-- name: LatestPoints :many
SELECT ts, value
FROM metric_sample
WHERE series_id = $1 AND ts > $2
ORDER BY ts DESC
LIMIT $3;

-- name: GetCollectionPlan :one
SELECT agent_metrics_enabled
FROM instance_collection_config
WHERE instance_id = $1;

-- name: SetAgentMetricsEnabled :exec
INSERT INTO instance_collection_config (instance_id, agent_metrics_enabled, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (instance_id)
DO UPDATE SET agent_metrics_enabled = EXCLUDED.agent_metrics_enabled,
              updated_at = EXCLUDED.updated_at;

-- name: ListTaskIntervals :many
SELECT task_id, interval_seconds
FROM collection_task_config
WHERE instance_id = $1
ORDER BY task_id;

-- name: SetTaskInterval :exec
INSERT INTO collection_task_config (instance_id, task_id, interval_seconds, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (instance_id, task_id)
DO UPDATE SET interval_seconds = EXCLUDED.interval_seconds,
              updated_at = EXCLUDED.updated_at;
