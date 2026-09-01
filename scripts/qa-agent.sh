#!/bin/sh
# Enroll the Agent for the QA instance and run it against the local target host.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
# Outside the repo on purpose: an rsync/--delete of the working tree must not
# take the master key, TLS material, or the running pids with it.
state="${QA_STATE_DIR:-$HOME/.dbs-monitor-qa}"
listen_port="${QA_PORT:-18443}"
base_url="https://127.0.0.1:$listen_port"
admin_password="${QA_ADMIN_PASSWORD:-admin}"
ca="$state/server-tls/ca.crt"
cookie="$state/cookie.txt"
instance_name="${QA_INSTANCE_NAME:-QA target pg17}"

api() { curl --noproxy '*' -s --cacert "$ca" -b "$cookie" "$@"; }

curl --noproxy '*' -sf --cacert "$ca" -c "$cookie" \
  -H 'Content-Type: application/json' -X POST "$base_url/api/v1/login" \
  --data "{\"username\":\"admin\",\"password\":\"$admin_password\"}" >/dev/null

instance_id=$(api "$base_url/api/v1/instances" \
  | INSTANCE_NAME="$instance_name" node -e 'let b="";process.stdin.on("data",c=>b+=c).on("end",()=>{const i=(JSON.parse(b)||[]).find(x=>x.name===process.env.INSTANCE_NAME);process.stdout.write(i?i.id:"")})')
if [ -z "$instance_id" ]; then echo "instance not found: $instance_name" >&2; exit 1; fi

# POST registration issues the token once; a re-run lands on 409, so rotate instead.
response=$(api -o "$state/agent-registration.json" -w '%{http_code}' \
  -X POST "$base_url/api/v1/instances/$instance_id/agent/registration")
if [ "$response" = "409" ]; then
  response=$(api -o "$state/agent-registration.json" -w '%{http_code}' \
    -X POST "$base_url/api/v1/instances/$instance_id/agent/token/rotation")
fi
if [ "$response" != "200" ]; then
  echo "agent registration failed ($response):" >&2; cat "$state/agent-registration.json" >&2; exit 1
fi

node -e 'process.stdout.write(JSON.parse(require("fs").readFileSync(process.argv[1],"utf8")).agent_token)' \
  "$state/agent-registration.json" > "$state/agent-token"
chmod 0600 "$state/agent-token"
rm -f "$state/agent-registration.json"

if [ -f "$state/agent.pid" ] && kill -0 "$(cat "$state/agent.pid")" 2>/dev/null; then
  kill "$(cat "$state/agent.pid")" 2>/dev/null || true
  sleep 1
fi
SERVER_URL="$base_url" \
INSTANCE_ID="$instance_id" \
AGENT_TOKEN_FILE="$state/agent-token" \
CA_FILE="$ca" \
PGDATA=/ \
nohup "$state/dbs-monitor-agent" >"$state/agent.log" 2>&1 &
agent_pid=$!
echo "$agent_pid" > "$state/agent.pid"

i=0
while [ "$i" -lt 30 ]; do
  reported=$(api "$base_url/api/v1/instances/$instance_id/agent/registration" \
    | node -e 'let b="";process.stdin.on("data",c=>b+=c).on("end",()=>{const r=JSON.parse(b);process.stdout.write(r.last_reported_at?r.state:"")})')
  if [ -n "$reported" ]; then echo "agent reporting (pid $agent_pid, state $reported)"; exit 0; fi
  if ! kill -0 "$agent_pid" 2>/dev/null; then tail -20 "$state/agent.log"; exit 1; fi
  sleep 1
  i=$((i + 1))
done
echo "agent did not report within 30s" >&2
tail -20 "$state/agent.log" >&2
exit 1
