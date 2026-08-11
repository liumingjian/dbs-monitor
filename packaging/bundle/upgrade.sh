#!/bin/sh
set -eu

install_root=/opt/dbs-monitor
bundle_root=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
backup_tmp=
stage_root=

cleanup() {
  if [ -n "$backup_tmp" ] && [ -e "$backup_tmp" ]; then
    rm -f -- "$backup_tmp"
  fi
  if [ -n "$stage_root" ] && [ -e "$stage_root" ]; then
    rm -rf -- "$stage_root"
  fi
}
trap cleanup EXIT INT TERM

if [ "$#" -ne 0 ]; then
  echo "usage: $0" >&2
  exit 2
fi
if [ "$(id -u)" -ne 0 ]; then
  echo "upgrade.sh must run as root" >&2
  exit 1
fi
for required in \
  "$bundle_root/bin/dbs-monitor-server" \
  "$bundle_root/pgsql/bin/postgres" \
  "$bundle_root/systemd/dbs-monitor-postgres.service" \
  "$bundle_root/systemd/dbs-monitor-server.service" \
  "$install_root/bin/dbs-monitor-server" \
  "$install_root/pgsql/bin/postgres" \
  "$install_root/etc/dbs-monitor.env"
do
  if [ ! -e "$required" ]; then
    echo "upgrade prerequisite is missing: $required" >&2
    exit 1
  fi
done

installed_major=$("$install_root/pgsql/bin/postgres" --version | sed -n 's/.* \([0-9][0-9]*\)\..*/\1/p')
bundle_major=$("$bundle_root/pgsql/bin/postgres" --version | sed -n 's/.* \([0-9][0-9]*\)\..*/\1/p')
if [ -z "$installed_major" ] || [ -z "$bundle_major" ]; then
  echo "could not determine installed and bundle PostgreSQL major versions" >&2
  exit 1
fi
if [ "$installed_major" != "$bundle_major" ]; then
  echo "PostgreSQL major-version upgrades require a separate migration (installed $installed_major, bundle $bundle_major)" >&2
  exit 1
fi

database_url=$(sed -n 's/^DATABASE_URL=//p' "$install_root/etc/dbs-monitor.env" | tail -n 1)
public_host=$(sed -n 's/^PUBLIC_HOST=//p' "$install_root/etc/dbs-monitor.env" | tail -n 1)
if [ -z "$database_url" ] || [ -z "$public_host" ]; then
  echo "DATABASE_URL and PUBLIC_HOST are required in dbs-monitor.env" >&2
  exit 1
fi

backup_dir="$install_root/backups"
mkdir -p "$backup_dir"
chown dbsmon:dbsmon "$backup_dir"
chmod 0700 "$backup_dir"

systemctl stop dbs-monitor-server.service

backup="$backup_dir/control-plane-$(date -u +%Y%m%dT%H%M%SZ).dump"
backup_tmp="$backup.tmp"
runuser -u dbsmon -- "$install_root/pgsql/bin/pg_dump" \
  --dbname="$database_url" \
  --format=custom \
  '--exclude-table-data=public.metric_sample*' \
  --file="$backup_tmp"
runuser -u dbsmon -- "$install_root/pgsql/bin/pg_restore" --list "$backup_tmp" >/dev/null
chmod 0600 "$backup_tmp"
mv "$backup_tmp" "$backup"
backup_tmp=
echo "control-plane backup created: $backup"

stage_root="$install_root/.upgrade-stage.$$"
mkdir "$stage_root"
cp -a "$bundle_root/bin" "$stage_root/bin"
cp -a "$bundle_root/pgsql" "$stage_root/pgsql"
chown -R dbsmon:dbsmon "$stage_root/bin" "$stage_root/pgsql"

old_bin="$install_root/.upgrade-old-bin.$$"
old_pgsql="$install_root/.upgrade-old-pgsql.$$"
systemctl stop dbs-monitor-postgres.service
mv "$install_root/bin" "$old_bin"
mv "$stage_root/bin" "$install_root/bin"
mv "$install_root/pgsql" "$old_pgsql"
mv "$stage_root/pgsql" "$install_root/pgsql"
cp "$bundle_root/systemd/dbs-monitor-postgres.service" /etc/systemd/system/
cp "$bundle_root/systemd/dbs-monitor-server.service" /etc/systemd/system/
systemctl daemon-reload
systemctl start dbs-monitor-postgres.service
systemctl start dbs-monitor-server.service

case "$public_host" in
  *:*) public_url="https://[$public_host]:8443/login" ;;
  *) public_url="https://$public_host:8443/login" ;;
esac
ready=0
for _ in $(seq 1 60); do
  if systemctl is-active --quiet dbs-monitor-server.service && \
     [ -s "$install_root/certs/ca.crt" ] && \
     curl --silent --fail --cacert "$install_root/certs/ca.crt" "$public_url" >/dev/null; then
    ready=1
    break
  fi
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  echo "upgraded server did not become ready; migration or startup failed" >&2
  echo "inspect journalctl -u dbs-monitor-server.service; control-plane backup: $backup" >&2
  echo "previous payload retained at $old_bin and $old_pgsql" >&2
  exit 1
fi

rm -rf -- "$old_bin" "$old_pgsql"
echo "DBS Monitor upgraded; goose migrations completed and the server is ready"
