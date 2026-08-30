#!/bin/sh
# Bring the whole system up for manual QA and leave it running.
# Mirrors test/acceptance/run.sh for TLS and containers, but starts the server
# detached instead of running a test binary.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
project=dbs-monitor-qa
# Outside the repo on purpose: an rsync/--delete of the working tree must not
# take the master key, TLS material, or the running pids with it.
state="${QA_STATE_DIR:-$HOME/.dbs-monitor-qa}"
tls_dir="${QA_TLS_HOME:-$HOME/.dbs-monitor-acceptance-tls}"
admin_password="${QA_ADMIN_PASSWORD:-qa-admin-password}"
listen_port="${QA_PORT:-18443}"
base_url="https://127.0.0.1:$listen_port"

mkdir -p "$state" "$state/diagnostics" "$state/credentials" "$tls_dir"
chmod 0700 "$state/credentials"

# --- TLS material for the platform database (LibreSSL-safe, same as acceptance) ---
if [ ! -f "$tls_dir/ca.crt" ] || [ ! -f "$tls_dir/ca.key" ] || ! openssl x509 -checkend 86400 -noout -in "$tls_dir/ca.crt" >/dev/null 2>&1; then
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
chmod 0600 "$tls_dir/ca.key"
openssl req -newkey rsa:2048 -nodes \
  -keyout "$tls_dir/server.key" -out "$tls_dir/server.csr" \
  -subj /CN=localhost >/dev/null 2>&1
printf 'subjectAltName=DNS:localhost,IP:127.0.0.1,DNS:acceptance-platform\n' > "$tls_dir/san.ext"
openssl x509 -req -in "$tls_dir/server.csr" \
  -CA "$tls_dir/ca.crt" -CAkey "$tls_dir/ca.key" -CAcreateserial \
  -out "$tls_dir/server.crt" -days 2 -extfile "$tls_dir/san.ext" >/dev/null 2>&1
chmod 0600 "$tls_dir/server.key"
chmod 0755 "$tls_dir"
chmod 0644 "$tls_dir/ca.crt" "$tls_dir/server.crt"
rm -f "$tls_dir/server.csr" "$tls_dir/san.ext"

# --- containers: platform database + one monitored target ---
cd "$root"
export ACCEPTANCE_PLATFORM_TLS_DIR="$tls_dir"
docker compose -p "$project" --profile acceptance up -d --wait acceptance-platform acceptance-target
echo "containers up"

# --- build ---
if [ ! -d web/node_modules ]; then (cd web && npm ci --silent); fi
(cd web && npm run build >"$state/web-build.log" 2>&1) || { tail -30 "$state/web-build.log"; exit 1; }
go build -tags embed_web -o "$state/dbs-monitor-server" ./cmd/monitor-server
go build -o "$state/dbs-monitor-agent" ./cmd/monitor-agent
echo "build ok"

# --- server config ---
cat > "$state/config.yaml" <<EOF
platform_database_url: "postgres://dbs_monitor:dbs_monitor@127.0.0.1:55442/dbs_monitor?search_path=dbsmon&sslmode=verify-full&sslrootcert=$tls_dir/ca.crt"
master_key_path: "$state/credentials"
agent_binary_dir: "$state"
local_disk_path: "$root"
diagnostic_bundle_directory: "$state/diagnostics"
EOF
chmod 0600 "$state/config.yaml"

# --- start the server detached ---
if [ -f "$state/server.pid" ] && kill -0 "$(cat "$state/server.pid")" 2>/dev/null; then
  kill "$(cat "$state/server.pid")" 2>/dev/null || true
  sleep 1
fi
DBS_MONITOR_CONFIG_FILE="$state/config.yaml" \
INITIAL_ADMIN_PASSWORD="$admin_password" \
LISTEN_ADDR="127.0.0.1:$listen_port" \
PUBLIC_HOST=127.0.0.1 \
CERT_DIR="$state/server-tls" \
nohup "$state/dbs-monitor-server" >"$state/server.log" 2>&1 &
server_pid=$!
echo "$server_pid" > "$state/server.pid"

ready=0
i=0
while [ "$i" -lt 120 ]; do
  if curl --noproxy '*' -sf --cacert "$state/server-tls/ca.crt" "$base_url/login" >/dev/null 2>&1; then ready=1; break; fi
  if ! kill -0 "$server_pid" 2>/dev/null; then tail -30 "$state/server.log"; exit 1; fi
  sleep 0.5
  i=$((i + 1))
done
if [ "$ready" -ne 1 ]; then tail -30 "$state/server.log"; exit 1; fi
echo "server up (pid $server_pid)"

# --- log in and register the monitored target if it is not there yet ---
cookie="$state/cookie.txt"
curl --noproxy '*' -sf --cacert "$state/server-tls/ca.crt" -c "$cookie" \
  -H 'Content-Type: application/json' -X POST "$base_url/api/v1/login" \
  --data "{\"username\":\"admin\",\"password\":\"$admin_password\"}" >/dev/null

existing=$(curl --noproxy '*' -sf --cacert "$state/server-tls/ca.crt" -b "$cookie" "$base_url/api/v1/instances" \
  | node -e 'let b="";process.stdin.on("data",c=>b+=c).on("end",()=>{const i=(JSON.parse(b)||[]).find(x=>x.name==="QA target pg17");process.stdout.write(i?i.id:"")})')
if [ -z "$existing" ]; then
  curl --noproxy '*' -sf --cacert "$state/server-tls/ca.crt" -b "$cookie" \
    -H 'Content-Type: application/json' -X POST "$base_url/api/v1/instances" \
    --data '{"name":"QA target pg17","host":"127.0.0.1","port":55447,"database":"monitored","username":"monitored","password":"monitored"}' >/dev/null
  echo "registered instance: QA target pg17"
else
  echo "instance already registered: QA target pg17"
fi

cat <<EOF

  URL       $base_url
  login     admin / $admin_password
  CA cert   $state/server-tls/ca.crt   (self-signed: accept the browser warning)
  logs      $state/server.log
  stop      sh scripts/qa-down.sh
EOF
