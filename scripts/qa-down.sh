#!/bin/sh
# Stop the manual-QA stack started by scripts/qa-up.sh.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
# Outside the repo on purpose: an rsync/--delete of the working tree must not
# take the master key, TLS material, or the running pids with it.
state="${QA_STATE_DIR:-$HOME/.dbs-monitor-qa}"
for name in agent server; do
  if [ -f "$state/$name.pid" ]; then
    kill "$(cat "$state/$name.pid")" 2>/dev/null || true
    rm -f "$state/$name.pid"
  fi
done
docker compose -p "${QA_PROJECT:-dbs-monitor-qa}" --profile acceptance down --volumes --remove-orphans >/dev/null 2>&1 || true
echo "qa stack down"
