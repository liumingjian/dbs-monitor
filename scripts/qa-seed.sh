#!/bin/sh
# Give the QA session something to look at: continuous pgbench load on the
# monitored target, the built-in rule set, and two rules tuned to actually fire.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
state="${QA_STATE_DIR:-$HOME/.dbs-monitor-qa}"
base_url="https://127.0.0.1:${QA_PORT:-18443}"
ca="$state/server-tls/ca.crt"
cookie="$state/cookie.txt"
target="${QA_PROJECT:-dbs-monitor-qa}-acceptance-target-1"
instance_name="${QA_INSTANCE_NAME:-QA target pg17}"

api() { curl --noproxy '*' -s --cacert "$ca" -b "$cookie" "$@"; }
post_json() { api -H 'Content-Type: application/json' -X POST "$@"; }

# --- load: pgbench inside the target container, detached ---
if ! docker exec -e PGPASSWORD=monitored "$target" \
  psql -h 127.0.0.1 -U monitored -d monitored -tAc "SELECT to_regclass('public.pgbench_accounts')" \
  | grep -q pgbench_accounts; then
  docker exec -e PGPASSWORD=monitored "$target" \
    pgbench -h 127.0.0.1 -U monitored -i -s 10 -q monitored >/dev/null 2>&1
  echo "pgbench initialised (scale 10)"
fi
if docker exec "$target" pgrep -f "pgbench -h" >/dev/null 2>&1; then
  echo "pgbench load already running"
else
  docker exec -d -e PGPASSWORD=monitored "$target" \
    pgbench -h 127.0.0.1 -U monitored -c 8 -j 2 -T 7200 monitored
  echo "pgbench load started (8 clients, 2h)"
fi

# --- alert rules ---
curl --noproxy '*' -sf --cacert "$ca" -c "$cookie" \
  -H 'Content-Type: application/json' -X POST "$base_url/api/v1/login" \
  --data "{\"username\":\"admin\",\"password\":\"${QA_ADMIN_PASSWORD:-admin}\"}" >/dev/null

instance_id=$(api "$base_url/api/v1/instances" \
  | INSTANCE_NAME="$instance_name" node -e 'let b="";process.stdin.on("data",c=>b+=c).on("end",()=>{const i=(JSON.parse(b)||[]).find(x=>x.name===process.env.INSTANCE_NAME);process.stdout.write(i?i.id:"")})')
if [ -z "$instance_id" ]; then echo "instance not found: $instance_name" >&2; exit 1; fi

existing_names=$(api "$base_url/api/v1/alert-rules" \
  | node -e 'let b="";process.stdin.on("data",c=>b+=c).on("end",()=>{const r=JSON.parse(b);const list=Array.isArray(r)?r:(r.alert_rules||r.rules||[]);process.stdout.write(list.map(x=>x.name).join("\n"))})')

# Built-in templates, scoped to this instance, at their shipped thresholds.
for template in $(api "$base_url/api/v1/alert-rule-templates" \
  | node -e 'let b="";process.stdin.on("data",c=>b+=c).on("end",()=>{process.stdout.write(JSON.parse(b).map(t=>t.id).join(" "))})'); do
  name=$(api "$base_url/api/v1/alert-rule-templates" \
    | TEMPLATE="$template" node -e 'let b="";process.stdin.on("data",c=>b+=c).on("end",()=>{const t=JSON.parse(b).find(x=>x.id===process.env.TEMPLATE);process.stdout.write(t?t.name:"")})')
  if printf '%s\n' "$existing_names" | grep -Fxq "$name"; then continue; fi
  code=$(post_json -o /dev/null -w '%{http_code}' \
    "$base_url/api/v1/alert-rule-templates/$template/alert-rules" \
    --data "{\"scope\":\"INSTANCES\",\"instance_ids\":[\"$instance_id\"],\"enabled\":true}")
  echo "template $template -> $code"
done

# Two rules the pgbench load will actually cross, so the alert pages are not empty.
add_rule() {
  rule_name=$1
  if printf '%s\n' "$existing_names" | grep -Fxq "$rule_name"; then
    echo "rule exists: $rule_name"
    return 0
  fi
  code=$(post_json -o "$state/rule.json" -w '%{http_code}' "$base_url/api/v1/alert-rules" --data "$2")
  if [ "$code" != "201" ]; then echo "rule $rule_name -> $code: $(cat "$state/rule.json")"; else echo "rule created: $rule_name"; fi
  rm -f "$state/rule.json"
}

add_rule "QA 触发用：TPS 高于 5" "{
  \"name\": \"QA 触发用：TPS 高于 5\", \"metric_id\": \"pg.tps\", \"aggregation\": \"avg\",
  \"operator\": \">=\", \"threshold\": 5, \"recovery_operator\": \"<=\", \"recovery_threshold\": 1,
  \"window_seconds\": 60, \"consecutive_count\": 1, \"recovery_consecutive_count\": 2,
  \"severity\": \"warning\", \"no_data_policy\": \"mark_no_data\", \"scope\": \"INSTANCES\",
  \"instance_ids\": [\"$instance_id\"], \"evaluation_interval_seconds\": 30, \"enabled\": true }"

add_rule "QA 触发用：连接数高于 5" "{
  \"name\": \"QA 触发用：连接数高于 5\", \"metric_id\": \"pg.connection.total\", \"aggregation\": \"max\",
  \"operator\": \">=\", \"threshold\": 5, \"recovery_operator\": \"<=\", \"recovery_threshold\": 2,
  \"window_seconds\": 60, \"consecutive_count\": 1, \"recovery_consecutive_count\": 2,
  \"severity\": \"critical\", \"no_data_policy\": \"mark_no_data\", \"scope\": \"INSTANCES\",
  \"instance_ids\": [\"$instance_id\"], \"evaluation_interval_seconds\": 30, \"enabled\": true }"
