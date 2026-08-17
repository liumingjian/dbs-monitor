#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
tls_dir=$(mktemp -d)
project=dbs-monitor-acceptance

cleanup() {
  docker compose -p "$project" down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$tls_dir"
}
trap cleanup EXIT INT TERM

openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "$tls_dir/ca.key" -out "$tls_dir/ca.crt" -days 2 \
  -subj /CN=dbs-monitor-acceptance-ca >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes \
  -keyout "$tls_dir/server.key" -out "$tls_dir/server.csr" \
  -subj /CN=localhost >/dev/null 2>&1
# SAN 走 -extfile 而不是 CSR 的 -addext + 签发端 -copy_extensions:
# macOS 自带 LibreSSL 无 -copy_extensions,会静默失败
printf 'subjectAltName=DNS:localhost,IP:127.0.0.1,DNS:acceptance-platform\n' > "$tls_dir/san.ext"
openssl x509 -req -in "$tls_dir/server.csr" \
  -CA "$tls_dir/ca.crt" -CAkey "$tls_dir/ca.key" -CAcreateserial \
  -out "$tls_dir/server.crt" -days 2 -extfile "$tls_dir/san.ext" >/dev/null
chmod 0600 "$tls_dir/server.key"
# 容器内非 root 用户(UID 65532)要能读到公开证书;mktemp 目录默认 0700 在 Linux 宿主上会被挡住
chmod 0755 "$tls_dir"
chmod 0644 "$tls_dir/ca.crt" "$tls_dir/server.crt"

cd "$root"
export ACCEPTANCE_PLATFORM_TLS_DIR="$tls_dir"
export ACCEPTANCE_COMPOSE_PROJECT="$project"
docker compose -p "$project" --profile acceptance down --volumes --remove-orphans >/dev/null 2>&1 || true
docker compose -p "$project" --profile acceptance --profile restore --profile smtp --profile webhook \
  up -d --wait acceptance-platform acceptance-target restore-target smtp-sink webhook-sink

ACCEPTANCE_CANDIDATE_SHA="$(git rev-parse HEAD)" \
ACCEPTANCE_PLATFORM_DATABASE_URL="postgres://dbs_monitor:dbs_monitor@127.0.0.1:55442/dbs_monitor?search_path=dbsmon&sslmode=verify-full&sslrootcert=$tls_dir/ca.crt" \
ACCEPTANCE_RESTORE_DATABASE_URL="postgres://dbs_monitor:dbs_monitor@127.0.0.1:55439/dbs_monitor?search_path=dbsmon&sslmode=verify-full&sslrootcert=$tls_dir/ca.crt" \
ACCEPTANCE_PG16_DATABASE_URL="postgres://dbs_monitor:dbs_monitor@127.0.0.1:55446/dbs_monitor?search_path=dbsmon&sslmode=verify-full&sslrootcert=$tls_dir/ca.crt" \
ACCEPTANCE_RECOVERY_DATABASE_URL="postgres://dbs_monitor:dbs_monitor@127.0.0.1:55446/dbs_monitor?search_path=dbsmon&sslmode=verify-full&sslrootcert=$tls_dir/ca.crt" \
ACCEPTANCE_COMPOSE_PROJECT="$project" \
ACCEPTANCE_SMTP_CA_FILE="$tls_dir/ca.crt" \
ACCEPTANCE_TARGET_PORT=55447 \
ACCEPTANCE_RESULT_PATH="$root/results/acceptance-result.json" \
PGHOST=127.0.0.1 \
PGPORT=55442 \
PGUSER=dbs_monitor \
PGDATABASE=dbs_monitor \
PGPASSWORD=dbs_monitor \
go test -count=1 -timeout 45m -tags acceptance ./test/acceptance
