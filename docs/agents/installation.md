# Agent installation

The instance access-settings page is the primary installation path. Register or re-enable the Agent, copy the one-time command, and run it as root on the database host. The installed service runs as the dedicated `dbs-monitor-agent` user and does not upgrade itself; rerun a newly issued installation command for an explicit upgrade.

The server-side `AGENT_BINARY_DIR` deployment setting must point to a directory containing executable `dbs-monitor-agent-linux-amd64` and `dbs-monitor-agent-linux-arm64` files. The server checks both files at startup; an unavailable or incomplete directory is reported by the distribution health fact and authenticated download requests return `503` instead of a silent `404`.

## Manual-copy fallback

Use this only when the host cannot reach the platform distribution endpoints. Copy the matching `dbs-monitor-agent-linux-amd64` or `dbs-monitor-agent-linux-arm64` release binary and the platform `ca.crt` through the approved delivery channel, then reproduce the installer-owned files:

- binary: `/opt/dbs-monitor-agent/bin/dbs-monitor-agent`, mode `0755`;
- CA: `/etc/dbs-monitor-agent/ca.crt`, mode `0644`;
- one-time registration token: `/etc/dbs-monitor-agent/token`, owned by `dbs-monitor-agent`, mode `0600`;
- systemd environment: `SERVER_URL`, `INSTANCE_ID`, `AGENT_TOKEN_FILE`, and `CA_FILE`, matching the generated `dbs-monitor-agent.service`.

Verify the copied CA's DER SHA-256 fingerprint against the access-settings page before starting the service. Installation is root-only; the service itself must remain non-root.
