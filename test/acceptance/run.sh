#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
# TLS 材料持久化(可用 ACCEPTANCE_TLS_HOME 覆盖):darwin 上 Go 忽略 SSL_CERT_FILE,
# 被测 server 的 SMTP STARTTLS 验证只认系统钥匙串;CA 固定下来,macOS 用户一次性
# sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain ~/.dbs-monitor-acceptance-tls/ca.crt
# 即可长期跑绿。CA 带 critical nameConstraints,只能给 localhost/127.0.0.1/acceptance-platform 签发,
# 信任它不会扩大到其它站点。服务端证书仍每次重签(2 天)。
tls_dir="${ACCEPTANCE_TLS_HOME:-$HOME/.dbs-monitor-acceptance-tls}"
mkdir -p "$tls_dir"
project=dbs-monitor-acceptance

cleanup() {
  docker compose -p "$project" down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -f "$tls_dir/server.csr" "$tls_dir/san.ext"
}
trap cleanup EXIT INT TERM

if [ ! -f "$tls_dir/ca.crt" ] || [ ! -f "$tls_dir/ca.key" ] || ! openssl x509 -checkend 86400 -noout -in "$tls_dir/ca.crt" >/dev/null 2>&1; then
  # -addext 在 macOS 自带 LibreSSL 上不可靠,扩展一律走 -config
  cat > "$tls_dir/ca.cnf" <<'EOF'
[req]
distinguished_name = dn
x509_extensions = v3_ca
prompt = no
[dn]
CN = dbs-monitor-acceptance-ca
[v3_ca]
basicConstraints = critical,CA:TRUE
keyUsage = critical,keyCertSign,cRLSign
nameConstraints = critical,permitted;DNS:localhost,permitted;DNS:acceptance-platform,permitted;IP:127.0.0.1/255.255.255.255
EOF
  openssl req -x509 -newkey rsa:2048 -nodes \
    -keyout "$tls_dir/ca.key" -out "$tls_dir/ca.crt" -days 3650 \
    -config "$tls_dir/ca.cnf" >/dev/null 2>&1
fi
# openssl 写私钥跟随 umask,持久目录是 0755,必须显式收紧
chmod 0600 "$tls_dir/ca.key"
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
go test -count=1 -timeout 45m -tags acceptance ${ACCEPTANCE_RUN:+-run "$ACCEPTANCE_RUN"} ./test/acceptance
