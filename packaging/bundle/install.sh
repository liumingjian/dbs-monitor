#!/bin/sh
set -eu

install_root=/opt/dbs-monitor
force=0
for argument in "$@"; do
  case "$argument" in
    --force) force=1 ;;
    *) echo "usage: $0 [--force]" >&2; exit 2 ;;
  esac
done

if [ "$(id -u)" -ne 0 ]; then
  echo "install.sh must run as root" >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64) expected_arch=amd64; minimum_glibc=2.17 ;;
  aarch64|arm64) expected_arch=arm64; minimum_glibc=2.28 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
bundle_arch=$(cat "$(dirname "$0")/ARCH")
if [ "$bundle_arch" != "$expected_arch" ]; then
  echo "bundle architecture $bundle_arch does not match host $expected_arch" >&2
  exit 1
fi

glibc_version=$(ldd --version 2>&1 | sed -n '1s/.* //p')
version_ge() {
  [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | sed -n '1p')" = "$1" ]
}
if [ -z "$glibc_version" ] || ! version_ge "$minimum_glibc" "$glibc_version"; then
  echo "glibc $minimum_glibc or newer is required (found ${glibc_version:-unknown})" >&2
  exit 1
fi

printf 'Data directory [/opt/dbs-monitor/data]: '
read -r data_dir
data_dir=${data_dir:-/opt/dbs-monitor/data}
printf 'Public platform IP or hostname: '
read -r public_host
if [ -z "$public_host" ]; then
  echo "public platform address is required for the TLS certificate SAN" >&2
  exit 1
fi

available_kb=$(df -Pk "$(dirname "$data_dir")" 2>/dev/null | tail -n 1 | tr -s ' ' | cut -d' ' -f4 || true)
required_kb=200000000
if [ -z "$available_kb" ] || [ "$available_kb" -lt "$required_kb" ]; then
  if [ "$force" -ne 1 ]; then
    echo "at least 200 GB free is required for the data directory; use --force only for non-production evaluation" >&2
    exit 1
  fi
  echo "WARNING: installing with less than 200 GB free" >&2
fi
memory_kb=$(sed -n 's/^MemTotal:[[:space:]]*\([0-9]*\).*/\1/p' /proc/meminfo)
[ -z "$memory_kb" ] || [ "$memory_kb" -ge 8000000 ] || echo "WARNING: less than 8 GB memory" >&2
cpu_count=$(getconf _NPROCESSORS_ONLN)
[ "$cpu_count" -ge 4 ] || echo "WARNING: fewer than 4 CPU cores" >&2

if ! id dbsmon >/dev/null 2>&1; then
  useradd --system --home-dir "$install_root" --shell /usr/sbin/nologin dbsmon
fi
mkdir -p "$install_root" "$install_root/bin" "$install_root/etc" "$install_root/run" "$install_root/certs" "$data_dir"
cp -a "$(dirname "$0")/bin/." "$install_root/bin/"
rm -rf "$install_root/pgsql"
cp -a "$(dirname "$0")/pgsql" "$install_root/pgsql"
chown -R dbsmon:dbsmon "$install_root" "$data_dir"
chmod 0700 "$install_root/run" "$install_root/certs" "$data_dir"

if [ ! -s "$data_dir/PG_VERSION" ]; then
  runuser -u dbsmon -- "$install_root/pgsql/bin/initdb" -D "$data_dir" --locale=C --encoding=UTF8 --auth-local=peer --auth-host=reject
fi
cat >>"$data_dir/postgresql.conf" <<EOF
listen_addresses = ''
unix_socket_directories = '$install_root/run'
EOF
cat >"$data_dir/pg_hba.conf" <<'EOF'
local all all peer
host all all 0.0.0.0/0 reject
host all all ::0/0 reject
EOF

cat >"$install_root/etc/dbs-monitor.env" <<EOF
PGDATA=$data_dir
DATABASE_URL=postgres:///dbs_monitor?host=$install_root/run&sslmode=disable
LISTEN_ADDR=:8443
PUBLIC_HOST=$public_host
CERT_DIR=$install_root/certs
CREDENTIALS_DIR=$install_root/etc/credentials
EOF
chmod 0600 "$install_root/etc/dbs-monitor.env"
chown dbsmon:dbsmon "$install_root/etc/dbs-monitor.env" "$data_dir/postgresql.conf" "$data_dir/pg_hba.conf"

cp "$(dirname "$0")/systemd/dbs-monitor-postgres.service" /etc/systemd/system/
cp "$(dirname "$0")/systemd/dbs-monitor-server.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now dbs-monitor-postgres.service

if ! runuser -u dbsmon -- "$install_root/pgsql/bin/psql" -h "$install_root/run" -d postgres -Atc "SELECT 1 FROM pg_database WHERE datname='dbs_monitor'" | grep -q 1; then
  runuser -u dbsmon -- "$install_root/pgsql/bin/createdb" -h "$install_root/run" dbs_monitor
fi
systemctl enable --now dbs-monitor-server.service

case "$public_host" in
  *:*) public_url="https://[$public_host]:8443/login" ;;
  *) public_url="https://$public_host:8443/login" ;;
esac
ready=0
for _ in $(seq 1 60); do
  if ! systemctl is-active --quiet dbs-monitor-postgres.service || ! systemctl is-active --quiet dbs-monitor-server.service; then
    sleep 1
    continue
  fi
  if [ -s "$install_root/certs/ca.crt" ] && curl --silent --fail --cacert "$install_root/certs/ca.crt" "$public_url" >/dev/null; then
    ready=1
    break
  fi
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  echo "services did not become ready at $public_url; inspect systemctl status and journalctl" >&2
  exit 1
fi

password_line=$(journalctl -u dbs-monitor-server.service --since '-2 minutes' --no-pager | grep 'initial administrator password (shown once):' | tail -n 1 || true)
if [ -z "$password_line" ]; then
  echo "server started, but the one-time administrator password was not found; inspect journalctl -u dbs-monitor-server" >&2
  exit 1
fi
printf '%s\n' "$password_line"
echo "DBS Monitor installed at https://$public_host:8443/"
