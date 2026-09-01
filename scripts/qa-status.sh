#!/bin/sh
# Report what a QA session most wants to know: agent state and whether each
# metric family actually has points.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
state="${QA_STATE_DIR:-$HOME/.dbs-monitor-qa}"
base_url="https://127.0.0.1:${QA_PORT:-18443}"
ca="$state/server-tls/ca.crt"
cookie="$state/cookie.txt"
curl --noproxy '*' -sf --cacert "$ca" -c "$cookie" \
  -H 'Content-Type: application/json' -X POST "$base_url/api/v1/login" \
  --data "{\"username\":\"admin\",\"password\":\"${QA_ADMIN_PASSWORD:-qa-admin-password}\"}" >/dev/null
curl --noproxy '*' -sf --cacert "$ca" -b "$cookie" "$base_url/api/v1/instances" > "$state/instances.json"
node -e '
const fs = require("fs")
const instances = JSON.parse(fs.readFileSync(process.argv[1], "utf8"))
for (const i of instances) {
  console.log(`instance ${i.name} id=${i.id} health=${i.health.status} agent=${i.agent_status} version=${i.agent_version ?? "-"} enhanced=${i.agent_metrics_enabled}`)
}
' "$state/instances.json"
id=$(node -e 'process.stdout.write(JSON.parse(require("fs").readFileSync(process.argv[1],"utf8"))[0].id)' "$state/instances.json")
from=$(node -e 'process.stdout.write(new Date(Date.now() - 600000).toISOString().replace(/\.\d+Z$/, "Z"))')
to=$(node -e 'process.stdout.write(new Date(Date.now() + 60000).toISOString().replace(/\.\d+Z$/, "Z"))')
for metric in pg.tps pg.connection.total host.cpu.usage_percent host.memory.usage_percent host.disk.usage_percent; do
  curl --noproxy '*' -sf --cacert "$ca" -b "$cookie" --get \
    --data-urlencode "metric=$metric" --data-urlencode "from=$from" \
    --data-urlencode "to=$to" --data-urlencode 'step=raw' \
    "$base_url/api/v1/instances/$id/metrics/series" \
    | node -e 'let b="";process.stdin.on("data",c=>b+=c).on("end",()=>{const m=JSON.parse(b).metrics[0];console.log(`${m.metric} points=${m.series.reduce((n,s)=>n+s.points.length,0)}`)})'
done
echo "monitoring page: $base_url/instances/$id/monitoring"
