#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cert_dir=$(mktemp -d)
credential_dir=$(mktemp -d)
platform_tls_dir=$(mktemp -d)
config_file=$(mktemp)
cookie_file=$(mktemp)
server_log=$(mktemp)
compose_project="dbs-monitor-e2e-$$"
server_pid=

cleanup() {
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  docker compose -p "$compose_project" down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$cert_dir" "$credential_dir" "$platform_tls_dir" "$config_file" "$cookie_file" "$server_log"
}
trap cleanup EXIT INT TERM

export PGPASSWORD="${PGPASSWORD:-dbs_monitor}"
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "$platform_tls_dir/ca.key" -out "$platform_tls_dir/ca.crt" -days 2 \
  -subj /CN=dbs-monitor-e2e-ca >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes \
  -keyout "$platform_tls_dir/server.key" -out "$platform_tls_dir/server.csr" \
  -subj /CN=localhost \
  -addext subjectAltName=DNS:localhost,IP:127.0.0.1,DNS:acceptance-platform >/dev/null 2>&1
openssl x509 -req -in "$platform_tls_dir/server.csr" \
  -CA "$platform_tls_dir/ca.crt" -CAkey "$platform_tls_dir/ca.key" -CAcreateserial \
  -out "$platform_tls_dir/server.crt" -days 2 -copy_extensions copy >/dev/null 2>&1
chmod 0600 "$platform_tls_dir/server.key"

export ACCEPTANCE_PLATFORM_TLS_DIR="$platform_tls_dir"
docker compose -p "$compose_project" --profile acceptance \
  up -d --wait acceptance-platform

platform_database_url="postgres://dbs_monitor:dbs_monitor@127.0.0.1:55442/dbs_monitor?search_path=dbsmon&sslmode=verify-full&sslrootcert=$platform_tls_dir/ca.crt"
printf 'platform_database_url: "%s"\nmaster_key_path: "%s"\nagent_binary_dir: "%s"\n' \
  "$platform_database_url" "$credential_dir" "$root" >"$config_file"
chmod 0600 "$config_file"

DBS_MONITOR_CONFIG_FILE="$config_file" \
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

curl --noproxy '*' --silent --fail --cacert "$cert_dir/ca.crt" -c "$cookie_file" \
  -H 'Content-Type: application/json' -X POST https://127.0.0.1:18443/api/v1/login \
  --data '{"username":"admin","password":"t11-playwright-password"}' >/dev/null
instance_id=$(node -e 'process.stdout.write(JSON.stringify({name:"T11 smoke instance",host:process.env.PGHOST || "localhost",port:Number(process.env.PGPORT || 55432),database:process.env.PGDATABASE || "dbs_monitor",username:process.env.PGUSER || "dbs_monitor",password:process.env.PGPASSWORD}))' \
  | curl --noproxy '*' --silent --fail --cacert "$cert_dir/ca.crt" -b "$cookie_file" \
  -H 'Content-Type: application/json' -X POST https://127.0.0.1:18443/api/v1/instances \
  --data-binary @- \
  | node -e "let body=''; process.stdin.on('data', chunk => body += chunk); process.stdin.on('end', () => process.stdout.write(JSON.parse(body).instance.id))")
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
