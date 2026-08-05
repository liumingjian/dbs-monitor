#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
database="dbs_monitor_e2e_$$"
cert_dir=$(mktemp -d)
server_log=$(mktemp)
server_pid=

cleanup() {
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  PGPASSWORD="${PGPASSWORD:-dbs_monitor}" psql -h "${PGHOST:-localhost}" -p "${PGPORT:-55432}" -U "${PGUSER:-dbs_monitor}" -d postgres -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS \"$database\" WITH (FORCE)" >/dev/null 2>&1 || true
  rm -rf "$cert_dir" "$server_log"
}
trap cleanup EXIT INT TERM

export PGPASSWORD="${PGPASSWORD:-dbs_monitor}"
psql -h "${PGHOST:-localhost}" -p "${PGPORT:-55432}" -U "${PGUSER:-dbs_monitor}" -d postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"$database\" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'" >/dev/null

DATABASE_URL="postgres://${PGUSER:-dbs_monitor}:$PGPASSWORD@${PGHOST:-localhost}:${PGPORT:-55432}/$database?sslmode=disable" \
INITIAL_ADMIN_PASSWORD=t11-playwright-password \
LISTEN_ADDR=127.0.0.1:18443 \
PUBLIC_HOST=127.0.0.1 \
CERT_DIR="$cert_dir" \
"$root/dbs-monitor-server" >"$server_log" 2>&1 &
server_pid=$!

ready=0
for _ in $(seq 1 60); do
  if curl --noproxy '*' --silent --fail --cacert "$cert_dir/ca.crt" https://127.0.0.1:18443/login >/dev/null 2>&1; then
    ready=1
    break
  fi
  if ! kill -0 "$server_pid" 2>/dev/null; then
    cat "$server_log" >&2
    exit 1
  fi
  sleep 0.25
done
if [ "$ready" -ne 1 ]; then
  cat "$server_log" >&2
  exit 1
fi

now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
instance_id=11111111-1111-4111-8111-111111111111
psql -h "${PGHOST:-localhost}" -p "${PGPORT:-55432}" -U "${PGUSER:-dbs_monitor}" -d "$database" -v ON_ERROR_STOP=1 <<SQL >/dev/null
INSERT INTO instance (id, name, host, port, database_name, username, password)
VALUES ('$instance_id', 'T11 smoke instance', '${PGHOST:-localhost}', ${PGPORT:-55432}, '${PGDATABASE:-dbs_monitor}', '${PGUSER:-dbs_monitor}', '$PGPASSWORD');
INSERT INTO metric_series (instance_id, metric_id, labels, labels_key, first_seen, last_seen)
VALUES ('$instance_id', 'pg.connection.total', '{}', '{}', '$now', '$now');
INSERT INTO metric_sample (series_id, ts, value)
SELECT series_id, '$now', 42 FROM metric_series WHERE instance_id = '$instance_id' AND metric_id = 'pg.connection.total';
SQL

cd "$root/web"
npm run e2e
