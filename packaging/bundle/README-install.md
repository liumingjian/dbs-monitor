# DBS Monitor offline installation

This archive is self-contained for the architecture named in `ARCH`. The target host does not need Docker, a package repository, Node.js, Go, or a pre-existing PostgreSQL installation.

## Prerequisites

- Linux with systemd and glibc at or above the architecture baseline (`amd64`: 2.17, `arm64`: 2.28)
- root for installation; both runtime services use the dedicated `dbsmon` user
- at least 200 GB free on the selected data filesystem (hard check; `--force` is evaluation-only)
- recommended 8 GB memory and 4 CPU cores
- an externally reachable IP or hostname chosen before installation; it becomes the server certificate SAN

## Install

```sh
sudo ./install.sh
```

The installer asks exactly for the data directory and public address. The third human item is an output: a random initial administrator password is printed from the first server start exactly once. Store it immediately.

PostgreSQL listens only on `/opt/dbs-monitor/run/`, uses peer authentication, and opens no TCP port. The platform opens HTTPS port 8443. Verify it with the generated CA rather than `--insecure`:

```sh
curl --cacert /opt/dbs-monitor/certs/ca.crt https://PUBLIC_HOST:8443/login
sudo -u dbsmon /opt/dbs-monitor/pgsql/bin/psql -h /opt/dbs-monitor/run dbs_monitor
systemctl status dbs-monitor-postgres dbs-monitor-server
ss -ltnp
```

Restart `dbs-monitor-server` and confirm its new journal entries do not print another initial password. Agent platform distribution, upgrades, rollback, high availability, and runtime disk-watermark behavior are intentionally outside T11 and deferred to R3. The T11 archive is a walking-skeleton package, not the complete production upgrade package.

## Master key rotation

Rotate the credential master key only during a maintenance window, with the platform database running and `dbs-monitor-server` stopped:

```sh
sudo systemctl stop dbs-monitor-server
sudo -u dbsmon sh -c 'set -a; . /opt/dbs-monitor/etc/dbs-monitor.env; exec /opt/dbs-monitor/bin/dbs-monitor-server rotate-master-key'
sudo systemctl start dbs-monitor-server
```

The command switches `current` atomically, re-encrypts every instance password in one database transaction, and deletes an old key only after verifying that no database row references it. If it is interrupted, leave all key files in place and run the same command again.
The server holds a database advisory lock for its lifetime. If the server is still running, or another rotation is active, the command rejects the rotation immediately without changing the keyring.

## Backup separation

A control-plane database backup and `/opt/dbs-monitor/etc/credentials` are two independent artifacts. Never include the credential keyring in the database backup archive. For same-host recovery retain the existing keyring; for a replacement host, transfer the matching keyring separately and restore its `dbsmon` ownership and `0600` file permissions before starting the server. An upgrade must preserve the keyring in place.

Both artifacts are required for recovery. If the keyring is missing, credential-dependent operations fail with a credential-key platform fault instead of generating a replacement or reporting a monitored database outage; the HTTP server remains available. A lost master key cannot be recovered; affected PostgreSQL credentials must be entered again.

## Platform host availability

The package does not provide external monitoring for a complete platform-host outage. If the host, its storage, or both systemd services are unavailable, the platform cannot report that failure itself. Before delivery, connect this installation to the customer's existing host or infrastructure monitoring and explicitly document that dependency.
