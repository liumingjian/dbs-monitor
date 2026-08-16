#!/bin/sh
set -eu

if [ "$#" -ne 4 ]; then
  echo "usage: install.sh SERVER_URL INSTANCE_ID CA_SHA256 BOOTSTRAP_CA" >&2
  exit 2
fi
if [ "$(id -u)" -ne 0 ]; then
  echo "Agent installation must run as root" >&2
  exit 1
fi

server_url=${1%/}
instance_id=$2
expected_ca_sha256=$3
bootstrap_ca=$4
IFS= read -r agent_token
if [ -z "$agent_token" ]; then
  echo "Agent token is required on standard input" >&2
  exit 1
fi
install_root=/opt/dbs-monitor-agent
config_root=/etc/dbs-monitor-agent
runtime_user=dbs-monitor-agent
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM

for command in curl install openssl sha256sum systemctl useradd; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "$command is required to install DBS Monitor Agent" >&2
    exit 1
  }
done

curl --fail --silent --show-error --cacert "$bootstrap_ca" "$server_url/api/agent/install/ca.crt" -o "$work/ca.crt"
actual_ca_sha256=$(openssl x509 -in "$work/ca.crt" -outform DER | sha256sum | cut -d' ' -f1)
if [ "$actual_ca_sha256" != "$expected_ca_sha256" ]; then
  echo "platform CA fingerprint mismatch" >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64) agent_arch=amd64 ;;
  aarch64|arm64) agent_arch=arm64 ;;
  *) echo "unsupported Agent architecture: $(uname -m)" >&2; exit 1 ;;
esac
curl_config=$work/curl.conf
umask 077
printf 'header = "Authorization: Bearer %s"\n' "$agent_token" >"$curl_config"
curl --fail --silent --show-error --cacert "$work/ca.crt" \
  --config "$curl_config" "$server_url/api/v1/agent/download?arch=linux/$agent_arch" -o "$work/dbs-monitor-agent"

if ! id "$runtime_user" >/dev/null 2>&1; then
  useradd --system --home-dir "$install_root" --shell /usr/sbin/nologin "$runtime_user"
fi
install -d -m 0755 "$install_root/bin" "$config_root"
install -m 0755 "$work/dbs-monitor-agent" "$install_root/bin/dbs-monitor-agent"
install -m 0644 "$work/ca.crt" "$config_root/ca.crt"
umask 077
printf '%s\n' "$agent_token" >"$config_root/token"
chown "$runtime_user:$runtime_user" "$config_root/token"
chmod 0600 "$config_root/token"

cat >"/etc/systemd/system/dbs-monitor-agent.service" <<EOF
[Unit]
Description=DBS Monitor Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$runtime_user
Group=$runtime_user
Environment=SERVER_URL=$server_url
Environment=INSTANCE_ID=$instance_id
Environment=AGENT_TOKEN_FILE=$config_root/token
Environment=CA_FILE=$config_root/ca.crt
ExecStart=$install_root/bin/dbs-monitor-agent
Restart=on-failure
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now dbs-monitor-agent.service
