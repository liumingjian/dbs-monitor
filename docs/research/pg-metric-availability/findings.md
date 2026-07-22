# PostgreSQL Metric Availability & Version Matrix (PG 12–17)

_Research task RT-A · PG 指标可得性与版本矩阵. Decision-support reference for defining monitoring metric semantics ("指标口径") across PostgreSQL 12 through 17._

This document pins down **which view/field exists in which version** and **what each field means**, for four areas: (1) replication-lag fields, (2) replication slots, (3) host CPU/memory under cgroups, and (4) `pg_monitor` role coverage. Facts are cited to version-specific `postgresql.org/docs` pages where a version boundary matters. Key takeaways up front: the time-based lag columns (`write/flush/replay_lag`) and LSN columns exist unchanged across all of 12–17, but the **standby-side view `pg_stat_wal_receiver` changed its column names in PG 13** (`received_lsn` → `written_lsn` + `flushed_lsn`); the slot-health columns **`wal_status`/`safe_wal_size` first appeared in PG 13**; and `pg_monitor` (via `pg_read_all_stats`) grants full read visibility of all the monitoring views but does **not** grant reset functions or table data.

---

## 1. Replication lag fields

### 1a. Primary side — `pg_stat_replication`

All columns below exist and are identically named/typed across **PG 12–17** (the time-lag columns were introduced in PG 10). Confirmed present in PG 12 ([PG12 monitoring-stats](https://www.postgresql.org/docs/12/monitoring-stats.html)) and unchanged in [current docs](https://www.postgresql.org/docs/current/monitoring-stats.html).

| Field | Type | Semantics (exact-per-docs) |
|---|---|---|
| `sent_lsn` | `pg_lsn` | Last WAL location **sent** on this connection. |
| `write_lsn` | `pg_lsn` | Last WAL location **written to disk** by this standby. |
| `flush_lsn` | `pg_lsn` | Last WAL location **flushed (fsync'd)** to disk by this standby. |
| `replay_lsn` | `pg_lsn` | Last WAL location **replayed into the database** on this standby. |
| `write_lag` | `interval` | Time between flushing recent WAL locally and receiving notification the standby **wrote** it (not yet flushed/applied). Gauges `synchronous_commit = remote_write` delay. |
| `flush_lag` | `interval` | Time between flushing locally and notification the standby **wrote+flushed** it (not yet applied). Gauges `synchronous_commit = on` delay. |
| `replay_lag` | `interval` | Time between flushing locally and notification the standby **wrote+flushed+applied** it. Gauges `synchronous_commit = remote_apply` delay; approximates when recent txns became visible to queries on the standby. |

**Caveat — the lag columns are NOT "predicted catch-up time" and revert to NULL on idle.** Per the docs: "If the standby server has entirely caught up with the sending server and there is no more WAL activity, the most recently measured lag times will continue to be displayed for a short time and then show NULL." They measure the write/flush/replay round-trip of the most recent WAL, not zero and not an ETA. ([current monitoring-stats](https://www.postgresql.org/docs/current/monitoring-stats.html))

### 1b. Standby side — `pg_stat_wal_receiver` (⚠ column rename in PG 13)

| Column | PG 12 | PG 13–17 | Notes |
|---|---|---|---|
| `received_lsn` | ✅ present | ❌ removed | PG12: "last WAL location received **and flushed** to disk". |
| `written_lsn` | ❌ | ✅ (new in 13) | "Last WAL location received and **written** to disk, but not flushed. Should not be used for data-integrity checks." |
| `flushed_lsn` | ❌ | ✅ (new in 13) | "Last WAL location received and **flushed** to disk." (This is the safe/durable position.) |
| `latest_end_lsn` | ✅ | ✅ | Last WAL location reported back to the origin WAL sender. |
| `sender_host` / `sender_port` | ✅ | ✅ | Present since PG 11. |

Sources: [PG12 monitoring-stats](https://www.postgresql.org/docs/12/monitoring-stats.html), [PG13 monitoring-stats](https://www.postgresql.org/docs/13/monitoring-stats.html), [current monitoring-stats](https://www.postgresql.org/docs/current/monitoring-stats.html).

### 1c. Time-based vs WAL-byte lag — which is more reliable

- **WAL-byte lag** is computed from LSN differences and is the more robust, always-available metric:
  - On the **primary**: `pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn)` per row of `pg_stat_replication` gives replay-lag bytes. Substitute `sent_lsn`/`write_lsn`/`flush_lsn` to isolate sent/write/flush lag. `pg_wal_lsn_diff()` returns `lsn1 - lsn2` in bytes. ([functions-admin](https://www.postgresql.org/docs/current/functions-admin.html))
  - On the **standby** (server is in recovery, so `pg_current_wal_lsn()` is **not** callable): use `pg_last_wal_receive_lsn()` (received+synced) and `pg_last_wal_replay_lsn()` (applied), with `pg_last_xact_replay_timestamp()` for a time proxy. `pg_wal_lsn_diff()` is one of the few backup-control functions explicitly allowed during recovery. ([functions-admin](https://www.postgresql.org/docs/current/functions-admin.html))
- **Time-based lag** (`*_lag` intervals) is meaningful only while WAL is actively flowing and reverts to NULL on a fully-replayed idle system (see 1a caveat). It is really a *synchronous-commit delay* metric, not a generic lag gauge.
- **Idle / standby-only situations:** During idle, byte-diff lag correctly reads ~0 while the interval columns go NULL. On the standby, a common wall-clock proxy is `now() - pg_last_xact_replay_timestamp()`, but note this reflects the timestamp of the last *replayed transaction* and inflates on an idle primary (no new commits to replay), so it can look like "lag" when there is none.

**Product-decision implication:** Define the canonical lag metric as **WAL bytes** (`pg_wal_lsn_diff`) computed from LSNs, because it is version-stable (12–17), always non-NULL, and works on both sides. Expose the `*_lag` intervals as a *secondary* "sync commit delay" signal, and never alert on them being NULL. Collector code must branch on version for the standby view: read `received_lsn` on PG 12 but `flushed_lsn` (durable) / `written_lsn` on PG ≥ 13.

---

## 2. Replication slots — `pg_replication_slots`

| Column | PG 12 | PG 13 | PG 14 | PG 15 | PG 16 | PG 17 | Meaning |
|---|---|---|---|---|---|---|---|
| `restart_lsn` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Oldest WAL LSN the slot's consumer may still need; WAL at/after this is retained (unless it falls > `max_slot_wal_keep_size` behind). NULL if never reserved. |
| `confirmed_flush_lsn` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | LSN up to which a **logical** slot's consumer confirmed receipt. NULL for physical slots. |
| `wal_status` | ❌ | ✅ **(new in 13)** | ✅ | ✅ | ✅ | ✅ | Availability of claimed WAL: `reserved` / `extended` / `unreserved` / `lost`. |
| `safe_wal_size` | ❌ | ✅ **(new in 13)** | ✅ | ✅ | ✅ | ✅ | Bytes that can still be written before this slot risks `lost`. NULL for lost slots and when `max_slot_wal_keep_size = -1`. |
| `two_phase` | ❌ | ❌ | ✅ **(new in 14)** | ✅ | ✅ | ✅ | Slot decodes prepared (2PC) transactions. Always false for physical slots. |
| `conflicting` | ❌ | ❌ | ❌ | ❌ | ✅ **(new in 16)** | ✅ | Logical slot was invalidated by a recovery conflict (logical decoding on standby). Always NULL for physical slots. |

Sources: [PG12](https://www.postgresql.org/docs/12/view-pg-replication-slots.html), [PG13](https://www.postgresql.org/docs/13/view-pg-replication-slots.html), [PG14](https://www.postgresql.org/docs/14/view-pg-replication-slots.html), [PG16](https://www.postgresql.org/docs/16/view-pg-replication-slots.html), [current](https://www.postgresql.org/docs/current/view-pg-replication-slots.html) view-pg-replication-slots.

**`wal_status` value meanings (PG 13+, verbatim from PG13 docs):**
- `reserved` — claimed files are within `max_wal_size`.
- `extended` — `max_wal_size` exceeded but files still retained (by the slot or by `wal_keep_size`).
- `unreserved` — slot no longer retains the required WAL; some files will be removed at the next checkpoint. Can return to `reserved`/`extended`.
- `lost` — required WAL files were removed; the slot is **no longer usable**.

([PG13 view-pg-replication-slots](https://www.postgresql.org/docs/13/view-pg-replication-slots.html))

**Computing retained-WAL bytes for a slot:** `pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)` on the primary gives the bytes of WAL held back by the slot. (On a standby holding slots, substitute `pg_last_wal_replay_lsn()` since `pg_current_wal_lsn()` is unavailable in recovery.) On PG ≥ 13, `safe_wal_size` and `wal_status` give the same danger signal directly and are preferable.

**"No replication slot" semantics:** `pg_replication_slots` returns **an empty result set** (zero rows) when no slots exist — there is no synthetic "0" row. A metric collector must treat *absence of a row* distinctly from *a row with a small/zero lag*; emitting 0 for "no slot" would mask the difference between "no consumer configured" and "consumer fully caught up." Likewise a slot with `active = false` / `active_pid = NULL` means the slot exists but nothing is currently streaming it (retention still accrues).

**Product-decision implication:** For PG ≥ 13, source slot-health from `wal_status` (categorical) + `safe_wal_size` (headroom bytes) directly — no arithmetic needed. For PG 12, there is no `wal_status`; you must derive retained bytes via `pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)` and compare against your own threshold, and there is no server-side notion of `lost` to read. Alert on `wal_status IN ('unreserved','lost')` (13+) and on empty-set vs zero being semantically different.

---

## 3. Host CPU / memory under containers & cgroups

This is OS-level, not PG-level. The central pitfall: **a monitoring agent that reads host-wide `/proc/stat` or `/proc/meminfo` and divides by host totals reports the wrong denominator for a containerized Postgres** — it must instead read the cgroup accounting files and use the cgroup *limit* as the denominator.

| Concern | Host view | cgroup v1 | cgroup v2 |
|---|---|---|---|
| Memory usage (numerator) | `/proc/meminfo` (whole node) | `memory.usage_in_bytes` | `memory.current` — "total memory currently used by the cgroup and its descendants" |
| Memory limit (denominator) | `MemTotal` (node RAM) | `memory.limit_in_bytes` | `memory.max` (hard cap; OOM kill above it) |
| Usage breakdown | — | `memory.stat` | `memory.stat` (`anon`, `file`, `inactive_file`, `slab`, `sock`) |
| CPU usage (numerator) | `/proc/stat` (all host cores) | `cpuacct.usage` | `cpu.stat` -> `usage_usec` |
| CPU limit (denominator) | host core count | `cpu.cfs_quota_us` / `cpu.cfs_period_us` | `cpu.max` = "MAX PERIOD"; usable cores = quota / period |
| Throttling signal | — | `cpu.stat`: `nr_periods`, `nr_throttled`, `throttled_time` | `cpu.stat`: `nr_periods`, `nr_throttled`, `throttled_usec` |

Kernel reference: [Control Group v2 — docs.kernel.org](https://docs.kernel.org/admin-guide/cgroup-v2.html).

**CPU% pitfalls:**
- **Wrong denominator:** the number of usable cores is `quota / period` (e.g. quota 50000us / period 100000us = 0.5 core). Container CPU% should be `usage_delta / (limit_cores × elapsed)`. Tools like `docker stats` deliberately scale against **host online cores**, which is why a container legitimately shows >100% (e.g. 250% = 2.5 cores) — do not reuse that number as "% of limit." ([Datadog: k8s CPU limits](https://www.datadoghq.com/blog/kubernetes-cpu-requests-limits/), [Last9 container CPU](https://last9.io/blog/monitoring-container-cpu-usage/))
- **CPU shares != cap:** `--cpu-shares`/`cpu.weight` is a *relative weight*, not a hard limit; building a percent-of-limit denominator on shares is meaningless. Only quota/period (and cpuset) impose hard caps.
- **Throttling hidden by averages:** a moderate average CPU% can conceal severe CFS throttling within the 100 ms window. Read `cpu.stat` and compute `nr_throttled / nr_periods` — a metric only visible at the cgroup level, never from host `/proc/stat`.

**Memory% pitfalls:**
- **Denominator:** use `memory.max`/`memory.limit_in_bytes` (container limit), not node `MemTotal`.
- **Cache/buffers:** `memory.current` includes page cache (`file`), tmpfs, slab, and — on **v2, kernel memory by default** — so raw usage overstates "app pressure." The "working set" approximation subtracts reclaimable cold cache: `working_set ≈ memory.current − inactive_file`. This mirrors host-side practice of not counting `buff/cache` against used memory. ([kernel cgroup-v2 docs](https://docs.kernel.org/admin-guide/cgroup-v2.html); working-set derivation: [Netdata cgroups](https://www.netdata.cloud/academy/diagnosing-linux-cgroups/))
- **v1 vs v2 divergence:** identical limits can behave differently because v2 charges kernel memory into `memory.current` by default — a workload fine on a v1 node can OOM on a v2 node. For OOM root-cause use raw `memory.current` vs `memory.max`; for alerting use the working-set (cache-subtracted) figure.

**Product-decision implication:** The agent must **detect cgroup version** (v1 vs v2, and whether running in a container at all) and switch file paths/denominators accordingly. Publish two memory series — raw `memory.current` (for OOM proximity) and working-set = `current − inactive_file` (for pressure/alerting) — and always divide by the cgroup limit, not host RAM. For CPU, publish `%-of-limit` using `quota/period` cores **and** a separate throttling ratio from `cpu.stat`; never surface only the host-scaled percentage.

---

## 4. `pg_monitor` role coverage

The predefined monitoring roles (`pg_monitor`, `pg_read_all_settings`, `pg_read_all_stats`, `pg_stat_scan_tables`) were introduced in **PG 10** and exist unchanged across **PG 12–17**. `pg_monitor` is a member of the other three. ([predefined-roles](https://www.postgresql.org/docs/current/predefined-roles.html))

| Sub-role (all in `pg_monitor`) | Grants |
|---|---|
| `pg_read_all_settings` | Read all configuration variables, incl. superuser-only GUCs. |
| `pg_read_all_stats` | Read all `pg_stat_*` views and stats-related extensions, incl. superuser-only info. |
| `pg_stat_scan_tables` | Execute monitoring functions that take `ACCESS SHARE` locks (e.g. `pgrowlocks`). |

**What `pg_monitor` DOES cover (all PG 12–17):**

| Target | Covered by `pg_monitor`? | Detail |
|---|---|---|
| `pg_stat_activity` — full column visibility | ✅ Yes | Ordinary users see only their own sessions' details (many columns NULL for other sessions); "Superusers and roles with privileges of built-in role `pg_read_all_stats` can see all the information about all sessions." ([monitoring-stats](https://www.postgresql.org/docs/current/monitoring-stats.html)) |
| `pg_stat_replication` | ✅ Yes | A `pg_stat_*` view -> covered by `pg_read_all_stats`. |
| `pg_replication_slots` | ✅ Yes | Readable; sensitive columns fully visible to `pg_read_all_stats` members. |
| `pg_stat_wal_receiver` | ✅ Yes | Covered by `pg_read_all_stats` (restricted columns like `conninfo` fully visible). |
| `pg_stat_statements` — query text/`queryid` of other users | ✅ Yes (if extension installed) | "only superusers and roles with privileges of the `pg_read_all_stats` role are allowed to see the SQL text and queryid of queries executed by other users." ([pg_stat_statements](https://www.postgresql.org/docs/current/pgstatstatements.html)) |

**What `pg_monitor` does NOT cover — gaps a monitoring account commonly hits:**
- **`pg_stat_statements` must still be installed.** `pg_monitor` grants *visibility*, not *existence*: the extension requires `shared_preload_libraries = 'pg_stat_statements'` (needs restart) **and** `CREATE EXTENSION pg_stat_statements` per database. Without both, there is nothing to read regardless of role. ([pg_stat_statements](https://www.postgresql.org/docs/current/pgstatstatements.html))
- **Reset functions are NOT granted.** `pg_stat_statements_reset()` (and `pg_stat_reset*`) are superuser-only by default and must be granted explicitly with `GRANT EXECUTE`. `pg_read_all_stats` briefly had this grant but it was reverted/back-patched; `pg_monitor` does not confer it. ([PG mailing-list thread](https://www.postgresql.org/message-id/CAJrrPGf5fCnKqXObpwGN9nMyD--tzOf-7LFCJiz59Z1wJ5qj9A%40mail.gmail.com))
- **No table/row data.** `pg_monitor` does not let you `SELECT` application table contents. Reading business tables needs `pg_read_all_data` (a *separate* role, added in **PG 14**, and not part of `pg_monitor`). ([predefined-roles](https://www.postgresql.org/docs/current/predefined-roles.html))
- **No connection/login by itself.** The role grants read privileges but you still need a login role with `CONNECT` on the target databases (and typically `LOGIN`).
- **Not a superuser substitute for everything.** Actions like `pg_stat_file()`/server-file reads, `ALTER SYSTEM`, terminating backends (`pg_signal_backend` is a different predefined role), or extension-specific admin functions are outside `pg_monitor`.

**Product-decision implication:** A monitoring service account = `GRANT pg_monitor` + `LOGIN` + `CONNECT` on each monitored DB. This is sufficient (PG 12–17) to read all replication/slot/activity/statements *metrics*. Explicitly document that (a) `pg_stat_statements` needs preload + `CREATE EXTENSION`, and (b) if the product offers a "reset stats" action, it needs an extra `GRANT EXECUTE` on the reset functions (or a superuser), which managed clouds (e.g. Azure Flexible Server) may block entirely.

---

## Sources

**PostgreSQL official docs (primary):**
- pg_stat_replication / pg_stat_wal_receiver / pg_stat_activity visibility — https://www.postgresql.org/docs/current/monitoring-stats.html · https://www.postgresql.org/docs/12/monitoring-stats.html · https://www.postgresql.org/docs/13/monitoring-stats.html
- pg_replication_slots (per version) — https://www.postgresql.org/docs/12/view-pg-replication-slots.html · https://www.postgresql.org/docs/13/view-pg-replication-slots.html · https://www.postgresql.org/docs/14/view-pg-replication-slots.html · https://www.postgresql.org/docs/16/view-pg-replication-slots.html · https://www.postgresql.org/docs/current/view-pg-replication-slots.html
- WAL/backup/recovery functions (pg_current_wal_lsn, pg_wal_lsn_diff, pg_last_wal_receive_lsn, pg_last_wal_replay_lsn, pg_last_xact_replay_timestamp) — https://www.postgresql.org/docs/current/functions-admin.html
- Predefined roles (pg_monitor et al.) — https://www.postgresql.org/docs/current/predefined-roles.html
- pg_stat_statements (visibility, install, reset) — https://www.postgresql.org/docs/current/pgstatstatements.html
- Reset-permission history (mailing list) — https://www.postgresql.org/message-id/CAJrrPGf5fCnKqXObpwGN9nMyD--tzOf-7LFCJiz59Z1wJ5qj9A%40mail.gmail.com

**Linux kernel & monitoring references (cgroups, secondary):**
- Control Group v2 — https://docs.kernel.org/admin-guide/cgroup-v2.html
- Netdata — diagnosing Linux cgroups (working-set / OOM) — https://www.netdata.cloud/academy/diagnosing-linux-cgroups/
- Datadog — Kubernetes CPU requests & limits — https://www.datadoghq.com/blog/kubernetes-cpu-requests-limits/
- Last9 — monitoring container CPU usage — https://last9.io/blog/monitoring-container-cpu-usage/
