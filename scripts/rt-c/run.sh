#!/bin/sh
set -eu

if [ "${RT_C_CONFIRM:-}" != "load-450m-points" ]; then
  echo "refusing to load approximately 453.6M points; set RT_C_CONFIRM=load-450m-points" >&2
  exit 2
fi
if [ -z "${RT_C_DATABASE_URL:-}" ]; then
  echo "RT_C_DATABASE_URL must point to a disposable, migrated T11 database" >&2
  exit 2
fi
if [ -z "${RT_C_DATA_PATH:-}" ] || [ ! -d "$RT_C_DATA_PATH" ]; then
  echo "RT_C_DATA_PATH must be an existing path on the PostgreSQL data filesystem" >&2
  exit 2
fi

root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
start=${RT_C_START:-2026-06-01T00:00:00Z}
days=${RT_C_DAYS:-30}
series_count=${RT_C_SERIES:-1750}
query_runs=${RT_C_QUERY_RUNS:-100}
control_runs=${RT_C_CONTROL_RUNS:-1000}
write_seconds=${RT_C_WRITE_SECONDS:-120}
disk_bytes=${RT_C_DELIVERY_DISK_BYTES:-200000000000}
run_id=$(date -u +%Y%m%dT%H%M%SZ)
results=${RT_C_RESULTS_DIR:-"$root/results/rt-c/$run_id"}
mkdir -p "$results"

psql_cmd="psql $RT_C_DATABASE_URL -X -v ON_ERROR_STOP=1"
pgbench_cmd="pgbench $RT_C_DATABASE_URL -n -c 1 -j 1"

existing=$($psql_cmd -Atc "SELECT count(*) FROM metric_sample")
if [ "$existing" != "0" ]; then
  echo "RT-C database is not empty (metric_sample has $existing rows)" >&2
  exit 2
fi
server_version_num=$($psql_cmd -Atc "SHOW server_version_num")
if [ "$server_version_num" -lt 170000 ] || [ "$server_version_num" -ge 180000 ]; then
  echo "RT-C requires PostgreSQL 17 (found server_version_num=$server_version_num)" >&2
  exit 2
fi
pg_data_path=$($psql_cmd -Atc "SHOW data_directory")
if [ "$RT_C_DATA_PATH" != "$pg_data_path" ]; then
  echo "RT_C_DATA_PATH must equal PostgreSQL data_directory ($pg_data_path)" >&2
  exit 2
fi
available_kb=$(df -Pk "$RT_C_DATA_PATH" | tail -n 1 | tr -s ' ' | cut -d' ' -f4)
required_kb=$((disk_bytes / 1024))
if [ "$available_kb" -lt "$required_kb" ]; then
  echo "RT-C data filesystem has less than $disk_bytes bytes free" >&2
  exit 2
fi

cat >"$results/manifest.json" <<JSON
{
  "git_revision": "$(git -C "$root" rev-parse HEAD)",
  "git_dirty": $(if git -C "$root" diff --quiet && [ -z "$(git -C "$root" ls-files --others --exclude-standard)" ]; then printf false; else printf true; fi),
  "started_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "window_start": "$start",
  "days": $days,
  "instances": 50,
  "series_count": $series_count,
  "points_per_series_per_day": 8640,
  "expected_total_points": $((series_count * 8640 * days)),
  "query_runs": $query_runs,
  "control_runs": $control_runs,
  "delivery_disk_bytes": $disk_bytes,
  "postgresql_server_version_num": $server_version_num,
  "data_path": "$(printf '%s' "$RT_C_DATA_PATH" | sed 's/"/\\"/g')",
  "uname": "$(uname -a | tr '"' "'")"
}
JSON
$psql_cmd -Atc "SELECT version()" >"$results/postgresql-version.txt"
$psql_cmd -Atc "SHOW ALL" >"$results/postgresql-settings.tsv"
df -k "$RT_C_DATA_PATH" >"$results/disk.txt"

$psql_cmd -v start="$start" -v days="$days" -v series_count="$series_count" <<'SQL'
INSERT INTO instance (id, name, host, port, database_name, username, password_ciphertext, password_key_version)
SELECT ('00000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid,
       'rt-c-' || lpad(i::text, 2, '0'), '127.0.0.1', 5432, 'rt_c', 'rt_c', decode('01', 'hex'), 1
FROM generate_series(1, 50) AS i;

INSERT INTO metric_series (instance_id, metric_id, labels, labels_key, first_seen, last_seen)
SELECT ('00000000-0000-4000-8000-' || lpad((((s - 1) / 35) + 1)::text, 12, '0'))::uuid,
       CASE s % 3 WHEN 0 THEN 'pg.availability.reachable' WHEN 1 THEN 'pg.connection.total' ELSE 'host.cpu.usage_percent' END,
       jsonb_build_object('rt_c_series', s), 'rt-c-' || s,
       :'start'::timestamptz, :'start'::timestamptz + make_interval(days => :'days'::integer)
FROM generate_series(1, :'series_count'::integer) AS s;

SELECT format(
  'CREATE TABLE %I PARTITION OF metric_sample FOR VALUES FROM (%L) TO (%L)',
  'metric_sample_' || to_char(day_start AT TIME ZONE 'UTC', 'YYYYMMDD'),
  day_start,
  day_start + interval '1 day'
)
FROM generate_series(
  :'start'::timestamptz,
  :'start'::timestamptz + make_interval(days => :'days'::integer - 1),
  interval '1 day'
) AS day_start
\gexec
SQL

sqlstate=$($psql_cmd -qAt -v start="$start" <<'SQL'
CREATE TEMP TABLE rt_c_sqlstate (value text);
CREATE TEMP TABLE rt_c_probe_start (value timestamptz);
INSERT INTO rt_c_probe_start VALUES (:'start'::timestamptz);
DO $$
DECLARE
  state text;
BEGIN
  BEGIN
    INSERT INTO metric_sample (series_id, ts, value)
    SELECT min(series_id), (SELECT value FROM rt_c_probe_start) - interval '1 day', 1
    FROM metric_series;
  EXCEPTION WHEN OTHERS THEN
    GET STACKED DIAGNOSTICS state = RETURNED_SQLSTATE;
    INSERT INTO rt_c_sqlstate VALUES (state);
  END;
END
$$;
SELECT value FROM rt_c_sqlstate;
SQL
)
printf '%s\n' "$sqlstate" >"$results/missing-partition-sqlstate.txt"
if [ "$sqlstate" != "23514" ]; then
  echo "missing partition SQLSTATE is $sqlstate, want 23514" >&2
  exit 1
fi

load_day=0
while [ "$load_day" -lt "$days" ]; do
  echo "loading day $((load_day + 1))/$days"
  $psql_cmd -v start="$start" -v day="$load_day" <<'SQL'
INSERT INTO metric_sample (series_id, ts, value)
SELECT series_id, tick, ((series_id * 17 + extract(epoch FROM tick)::bigint) % 1000)::double precision / 10
FROM metric_series
CROSS JOIN generate_series(
  :'start'::timestamptz + make_interval(days => :'day'::integer),
  :'start'::timestamptz + make_interval(days => :'day'::integer + 1) - interval '10 seconds',
  interval '10 seconds'
) AS tick;
SQL
  load_day=$((load_day + 1))
done

expected_points=$((series_count * 8640 * days))
actual_points=$($psql_cmd -Atc "SELECT count(*) FROM metric_sample")
actual_series=$($psql_cmd -Atc "SELECT count(*) FROM metric_series")
partition_count=$($psql_cmd -v start="$start" -v days="$days" -At <<'SQL'
SELECT count(*)
FROM pg_inherits inheritance
JOIN pg_class child ON child.oid = inheritance.inhrelid
WHERE inheritance.inhparent = 'metric_sample'::regclass
  AND child.relname >= 'metric_sample_' || to_char(:'start'::timestamptz AT TIME ZONE 'UTC', 'YYYYMMDD')
  AND child.relname < 'metric_sample_' || to_char(
    (:'start'::timestamptz + make_interval(days => :'days'::integer)) AT TIME ZONE 'UTC',
    'YYYYMMDD'
  );
SQL
)
printf 'actual_total_points,actual_series,partitions\n%s,%s,%s\n' "$actual_points" "$actual_series" "$partition_count" >"$results/counts.csv"
if [ "$actual_points" != "$expected_points" ] || [ "$actual_series" != "$series_count" ] || [ "$partition_count" != "$days" ]; then
  echo "RT-C load verification failed: points=$actual_points/$expected_points series=$actual_series/$series_count partitions=$partition_count/$days" >&2
  exit 1
fi

query_series=$($psql_cmd -Atc "SELECT series_id FROM metric_series WHERE metric_id='pg.connection.total' ORDER BY series_id LIMIT 1")
cat >"$results/query.sql" <<SQL
SELECT date_bin('1 minute', ts, '2000-01-01'::timestamptz), avg(value)::double precision
FROM metric_sample
WHERE series_id = $query_series
  AND ts >= '$start'::timestamptz
  AND ts < '$start'::timestamptz + interval '24 hours'
GROUP BY 1 ORDER BY 1;
SQL
$psql_cmd -f "$results/query.sql" >/dev/null
$psql_cmd -c "EXPLAIN (ANALYZE, BUFFERS) $(tr '\n' ' ' <"$results/query.sql")" >"$results/query-explain.txt"
(cd "$results" && $pgbench_cmd -t "$query_runs" -f query.sql -l --log-prefix=query- >/dev/null)

$psql_cmd -v start="$start" -v days="$days" --csv <<'SQL' >"$results/capacity.csv"
SELECT child.relname AS partition,
       pg_relation_size(child.oid) AS heap_bytes,
       pg_indexes_size(child.oid) AS index_bytes,
       pg_total_relation_size(child.oid) AS total_bytes
FROM pg_inherits inheritance
JOIN pg_class parent ON parent.oid = inheritance.inhparent
JOIN pg_class child ON child.oid = inheritance.inhrelid
WHERE parent.relname = 'metric_sample'
  AND child.relname >= 'metric_sample_' || to_char(:'start'::timestamptz AT TIME ZONE 'UTC', 'YYYYMMDD')
  AND child.relname < 'metric_sample_' || to_char(
    (:'start'::timestamptz + make_interval(days => :'days'::integer)) AT TIME ZONE 'UTC',
    'YYYYMMDD'
  )
ORDER BY child.relname;
SQL

cat >"$results/control.sql" <<'SQL'
\set target random(1, 50)
BEGIN;
INSERT INTO instance_collect_state (instance_id, source, last_success_at)
VALUES (('00000000-0000-4000-8000-' || lpad(:target::text, 12, '0'))::uuid, 'SERVER_DIRECT', clock_timestamp())
ON CONFLICT (instance_id, source) DO UPDATE
SET last_success_at = EXCLUDED.last_success_at, last_error_code = NULL, last_error_message = NULL;
SELECT last_success_at, last_error_code FROM instance_collect_state
WHERE instance_id = ('00000000-0000-4000-8000-' || lpad(:target::text, 12, '0'))::uuid
  AND source = 'SERVER_DIRECT';
COMMIT;
SQL
cat >"$results/write-pressure.sql" <<SQL
INSERT INTO metric_sample (series_id, ts, value)
SELECT series_id,
       '$start'::timestamptz + random() * make_interval(days => $days),
       random() * 100
FROM metric_series;
SQL
(cd "$results" && $pgbench_cmd -t "$control_runs" -f control.sql -l --log-prefix=control-baseline- >/dev/null)
(cd "$results" && $pgbench_cmd -T "$write_seconds" -f write-pressure.sql >/dev/null) &
writer_pid=$!
sleep 2
(cd "$results" && $pgbench_cmd -t "$control_runs" -f control.sql -l --log-prefix=control-pressure- >/dev/null)
wait "$writer_pid"

python3 "$root/scripts/rt-c/summarize.py" "$results" "$disk_bytes"
