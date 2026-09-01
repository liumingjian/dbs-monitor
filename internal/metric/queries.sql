-- name: UpsertSeries :one
INSERT INTO metric_series (instance_id, metric_id, database_name, labels, labels_key, last_seen)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (instance_id, metric_id, database_name, labels_key)
DO UPDATE SET last_seen = GREATEST(metric_series.last_seen, EXCLUDED.last_seen)
RETURNING series_id;

-- name: SeriesForMetric :many
SELECT series_id, database_name, labels
FROM metric_series
WHERE instance_id = $1 AND metric_id = $2
ORDER BY database_name, series_id;

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
INSERT INTO collection_task_config (instance_id, task_id, interval_seconds, updated_by, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (instance_id, task_id)
DO UPDATE SET interval_seconds = EXCLUDED.interval_seconds,
              updated_by = EXCLUDED.updated_by,
              updated_at = EXCLUDED.updated_at;

-- name: RecentValuePerSeriesForMetric :many
-- 一个指标在全机群的最近读数，每条序列一行（磁盘按挂载点分序列，实例级的取值由调用方
-- 在 Go 里收敛）。窗口是必需的：没有下限就会把几个月前停止上报的实例算成「现在的读数」。
--
-- 名字里刻意不出现 latest sample time / watermark：CONTEXT.md 把这两个说法留给了
-- 采集完整性水位，那是「每个到期义务都满足到了哪一刻」，不是「某条序列最后一个点的值」。
SELECT DISTINCT ON (series.series_id)
       series.instance_id,
       series.series_id,
       sample.ts,
       sample.value
FROM metric_series series
JOIN metric_sample sample ON sample.series_id = series.series_id
WHERE series.metric_id = sqlc.arg(metric_id) AND sample.ts >= sqlc.arg(since)
ORDER BY series.series_id, sample.ts DESC;
