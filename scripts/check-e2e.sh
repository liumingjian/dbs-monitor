#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
database="dbs_monitor_e2e_$$"
cert_dir=$(mktemp -d)
credential_dir=$(mktemp -d)
cookie_file=$(mktemp)
server_log=$(mktemp)
server_pid=

cleanup() {
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  PGPASSWORD="${PGPASSWORD:-dbs_monitor}" psql -h "${PGHOST:-localhost}" -p "${PGPORT:-55432}" -U "${PGUSER:-dbs_monitor}" -d postgres -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS \"$database\" WITH (FORCE)" >/dev/null 2>&1 || true
  rm -rf "$cert_dir" "$credential_dir" "$cookie_file" "$server_log"
}
trap cleanup EXIT INT TERM

export PGPASSWORD="${PGPASSWORD:-dbs_monitor}"
psql -h "${PGHOST:-localhost}" -p "${PGPORT:-55432}" -U "${PGUSER:-dbs_monitor}" -d postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"$database\" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'" >/dev/null

DATABASE_URL="postgres://${PGUSER:-dbs_monitor}:$PGPASSWORD@${PGHOST:-localhost}:${PGPORT:-55432}/$database?sslmode=disable" \
INITIAL_ADMIN_PASSWORD=t11-playwright-password \
LISTEN_ADDR=127.0.0.1:18443 \
PUBLIC_HOST=127.0.0.1 \
CERT_DIR="$cert_dir" \
CREDENTIALS_DIR="$credential_dir" \
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

curl --noproxy '*' --silent --fail --cacert "$cert_dir/ca.crt" -c "$cookie_file" \
  -H 'Content-Type: application/json' -X POST https://127.0.0.1:18443/api/v1/login \
  --data '{"username":"admin","password":"t11-playwright-password"}' >/dev/null
instance_id=$(node -e 'process.stdout.write(JSON.stringify({name:"T11 smoke instance",host:process.env.PGHOST || "localhost",port:Number(process.env.PGPORT || 55432),database:process.env.PGDATABASE || "dbs_monitor",username:process.env.PGUSER || "dbs_monitor",password:process.env.PGPASSWORD}))' \
  | curl --noproxy '*' --silent --fail --cacert "$cert_dir/ca.crt" -b "$cookie_file" \
  -H 'Content-Type: application/json' -X POST https://127.0.0.1:18443/api/v1/instances \
  --data-binary @- \
  | node -e "let body=''; process.stdin.on('data', chunk => body += chunk); process.stdin.on('end', () => process.stdout.write(JSON.parse(body).instance.id))")
agent_token=$(curl --noproxy '*' --silent --fail --cacert "$cert_dir/ca.crt" -b "$cookie_file" \
  -X POST "https://127.0.0.1:18443/api/v1/instances/$instance_id/agent/registration" \
  | node -e "let body=''; process.stdin.on('data', chunk => body += chunk); process.stdin.on('end', () => process.stdout.write(JSON.parse(body).agent_token))")
E2E_INSTANCE_ID="$instance_id" E2E_NOW="$now" node -e 'process.stdout.write(JSON.stringify({instance_id:process.env.E2E_INSTANCE_ID,agent_version:"2.0.0",timestamp:process.env.E2E_NOW,metrics:[{metric:"host.cpu.usage_percent",value:42}]}))' \
  | curl --noproxy '*' --silent --fail --cacert "$cert_dir/ca.crt" \
  -H "Authorization: Bearer $agent_token" -H 'Content-Type: application/json' \
  -X POST https://127.0.0.1:18443/api/agent/v1/report --data-binary @- >/dev/null

series_from=$(date -u -d '1 minute ago' +%Y-%m-%dT%H:%M:%SZ)
series_to=$(date -u -d '1 minute' +%Y-%m-%dT%H:%M:%SZ)
samples_ready=0
for _ in $(seq 1 80); do
  if curl --noproxy '*' --silent --fail --cacert "$cert_dir/ca.crt" -b "$cookie_file" --get \
    --data-urlencode 'metric=pg.tps' \
    --data-urlencode "from=$series_from" \
    --data-urlencode "to=$series_to" \
    --data-urlencode 'step=raw' \
    "https://127.0.0.1:18443/api/v1/instances/$instance_id/metrics/series" \
    | node -e '
let body = ""
process.stdin.on("data", (chunk) => { body += chunk })
process.stdin.on("end", () => {
  const metric = JSON.parse(body).metrics[0]
  const hasPoints = metric.series.some((series) => series.points.length > 0)
  process.exit(hasPoints ? 0 : 1)
})
'; then
    samples_ready=1
    break
  fi
  sleep 0.25
done
if [ "$samples_ready" -ne 1 ]; then
  echo "real pg.tps samples did not arrive" >&2
  cat "$server_log" >&2
  exit 1
fi

cd "$root/web"
npm run e2e
