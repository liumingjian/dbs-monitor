# RT-B · Slow-Query / Enhanced-Monitoring Data Sources for Private PostgreSQL

**Scope:** Compare realistic data sources for slow-query and "增强监控" (enhanced) monitoring in a **private, self-hosted / on-prem** PostgreSQL deployment where we may have **restricted permissions** and/or **no ability to restart the server**. Feeds a downstream decision on whether/how slow-query and enhanced monitoring enter the MVP. All version-of-introduction notes and behavioral claims are cited inline to postgresql.org.

## Summary

There are three practical sources. **`pg_stat_statements`** is the richest (per-normalized-query aggregate stats: calls, exec/plan time, rows, WAL) and is safe by default because it normalizes literals, but it must be listed in `shared_preload_libraries`, which **requires a server restart**, and `CREATE EXTENSION` requires elevated privilege — so it may be unavailable under no-restart / restricted-permission constraints. **Log-based** slow queries (`log_min_duration_statement`, optionally `auto_explain`) capture *every* slow statement including its plan, are runtime-togglable without a restart, but log **raw SQL with literal values** (a masking/PII problem) and are not real-time (log-parse latency). **`pg_stat_activity` sampling** needs no setup and no restart, catches only currently-running (`state='active'`) statements at each sample tick, and misses short or already-completed queries. For an MVP under restricted/no-restart assumptions, **prefer `pg_stat_activity` sampling as the always-available baseline, and opportunistically use `pg_stat_statements` when it is already loaded**; treat log-based/`auto_explain` as an optional, later, opt-in enhancement due to masking and latency.

---

## Source 1 — `pg_stat_statements` extension

### Enabling: preload (restart) vs. CREATE EXTENSION — two distinct steps

- **Loading the library requires a restart.** "The module must be loaded by adding `pg_stat_statements` to `shared_preload_libraries` in `postgresql.conf`, because it requires additional shared memory. This means that a server restart is needed to add or remove the module." [1] There is **no runtime-only way** to load it (unlike a plain `LOAD`, which does not work for it because it needs shared memory allocated at startup). [1]
- **Query-id computation must also be on.** The module is only active if a query identifier is being computed, "which is done automatically if `compute_query_id` is set to `auto` or `on`". `compute_query_id` defaults to `auto`, which auto-enables when the extension is loaded (this move of the query-id hash into the core server is a **PG14** change). [1][5]
- **`CREATE EXTENSION` is a separate, per-database step.** Loading via `shared_preload_libraries` enables global tracking across all databases; the **views/functions** (`pg_stat_statements`, `pg_stat_statements_info`) "can be enabled for a specific database with `CREATE EXTENSION pg_stat_statements`." [1] So: preload = collect data (needs restart + config file edit); `CREATE EXTENSION` = expose the view in a DB (needs privilege to create extensions). Both are elevated operations; neither is available to an unprivileged, no-restart user.

### Overhead / performance characteristics

- **Shared memory is always consumed once loaded**, proportional to `pg_stat_statements.max`, "even if `pg_stat_statements.track` is set to `none`." [1]
- **`pg_stat_statements.max`** (default **5000**) caps the number of tracked normalized statements; when exceeded, least-executed entries are discarded. Changing it requires a server restart. [1]
- **`pg_stat_statements.track`** (default `top`) selects `top` (client-issued only), `all` (include nested statements inside functions/procedures), or `none`. [1]
- **`pg_stat_statements.track_planning`** (default **off**, introduced in **PG13**) tracks planning time; the docs explicitly warn it "may incur a noticeable performance penalty, especially when statements with identical query structure are executed by many concurrent connections which compete to update a small number of `pg_stat_statements` entries." [1][4]
- **`pg_stat_statements.track_utility`** (default `on`) and **`pg_stat_statements.save`** (default `on`, persist across restarts) round out the main knobs. [1]

### Key fields and cross-version changes

- **PG13 split `total_time` into `total_plan_time` + `total_exec_time`** (with `mean_/min_/max_/stddev_` variants for each). Release note: "Allow pg_stat_statements to optionally track the planning time of statements … Previously only execution time was tracked." [4] Downstream tools that assumed a single `total_time` column must branch on version.
- **PG13 added WAL columns** `wal_records`, `wal_fpi`, `wal_bytes`. Release note: "Allow EXPLAIN, auto_explain, autovacuum, and pg_stat_statements to track WAL usage statistics." [4]
- **PG14 added the `toplevel` boolean** — top-level vs. nested statements are now tracked separately: "Cause pg_stat_statements to track top and nested statements separately … it seems more useful to separate such usages." [5]
- **PG14 exposed `queryid` more widely.** With `compute_query_id` enabled, the query id is shown in `pg_stat_activity`, `EXPLAIN VERBOSE`, csvlog, and optionally `log_line_prefix` (`%Q`). [5] This lets you *join* a running session (`pg_stat_activity`) to its aggregate stats (`pg_stat_statements`) by `queryid` on PG14+.
- **Core aggregate columns:** `queryid`, `query` (representative text), `calls`, `plans`, `rows`, `total_exec_time`/`mean_exec_time`/`min_exec_time`/`max_exec_time`, `total_plan_time`/`mean_plan_time`, `stats_since`, `minmax_stats_since`. [1]
- **`queryid` stability caveats:** derived from the post-parse-analysis tree, sensitive to object OIDs and machine architecture; assumed stable across **minor** releases but **"It is not safe to assume that `queryid` will be stable across major versions of PostgreSQL."** [1] So `queryid` is not a durable cross-upgrade key.

### Query-text normalization / masking behavior (security-positive)

- **Literals are normalized out by default.** "Typically, two queries will be considered the same … if they are semantically equivalent except for the values of literal constants." When a constant is ignored for matching, "the constant is replaced by a parameter symbol, such as `$1`, in the `pg_stat_statements` display." [1] This means **raw literal values (potential PII) are generally NOT stored** — a major advantage over log-based capture.
- **Long `IN` lists are squashed** to `... a IN ($1 /*, ... */)` in the representative text (a recent display improvement). [1]
- **What still leaks:** the stored representative `query` text is that of the first statement with a given `queryid`; identifiers (table/column names) and the query shape are visible. Access to SQL text and `queryid` of **other users'** queries is restricted to "superusers and roles with privileges of the `pg_read_all_stats` role"; other users see stats but not others' text. [1] Note: constants inside **utility** commands and certain non-normalizable spots are not parameterized, so it is not a guaranteed PII scrubber — but it is far safer than raw logs.

---

## Source 2 — Log-based slow queries (`log_min_duration_statement` + `auto_explain`)

### `log_min_duration_statement`

- **What it does:** "Causes the duration of each completed statement to be logged if the statement ran for at least the specified amount of time. For example, if you set it to `250ms` then all SQL statements that run 250ms or longer will be logged." [2]
- **Default `-1` (disabled); `0` logs all; unit is milliseconds if unqualified.** [2]
- **Runtime-togglable without restart:** it is a `SET`-able GUC (changeable by superusers or roles granted `SET` privilege on it, and via `reload`), so unlike preloading an extension it does **not** require a server restart. [2]
- **Sampling variants** `log_min_duration_sample` + `log_statement_sample_rate` let you log only a fraction of qualifying statements to control volume. [2]
- **Masking problem:** the log message contains the **raw statement text with literal values** (and for extended-protocol clients the Bind parameter values), so logs can contain PII/secrets and require downstream scrubbing. [2]

### `auto_explain` module

- **Loading is more flexible than pg_stat_statements:** it can be preloaded via `shared_preload_libraries` **or** `session_preload_libraries`, **or** loaded per-session at runtime with `LOAD 'auto_explain';` (superuser). "More typical usage is to preload it into some or all sessions." [3] The session-level `LOAD` path means it *can* be used without a full restart, but still needs elevated privilege.
- **`auto_explain.log_min_duration`** (default `-1` = off; `0` = all) sets the threshold to log the **execution plan** of slow statements. [3]
- **`auto_explain.log_analyze`** (default off) logs `EXPLAIN ANALYZE`-style output. Docs warn strongly: "When this parameter is on, per-plan-node timing occurs for all statements executed, whether or not they run long enough to actually get logged. This can have an extremely negative impact on performance." [3] `auto_explain.log_timing` (default on) can be turned off to reduce that cost at the price of losing per-node times. [3]
- **`auto_explain.sample_rate`** (default 1) explains only a fraction of statements per session to bound overhead. [3]

**Coverage / real-time-ness:** Log-based capture covers **every** statement over the threshold (not sampled unless you opt into sampling), including plans (with `auto_explain`) — this is its strength. But it is **not real-time**: entries appear only after the statement *completes*, and monitoring depends on tailing/parsing log files (parse + ship latency, format fragility). Overhead is low for `log_min_duration_statement` alone; `auto_explain.log_analyze`/`log_timing` can be significant. [2][3]

---

## Source 3 — Active-session inference via `pg_stat_activity` sampling

- **Snapshot of current activity, one row per backend.** The `query` column: "Text of this backend's most recent query. If `state` is `active` this field shows the currently executing query. In all other states, it shows the last query that was executed." [6] So sampling for `state='active'` rows periodically approximates "what long-running queries are in flight now."
- **What it catches:** long-running / currently-executing statements that happen to be active at a sample tick, plus `idle in transaction` offenders. Current-query info from `track_activities` is "always up-to-date" (real-time). [6]
- **What it CANNOT catch:** any query that starts and finishes **between** sample ticks (short/fast queries) is missed entirely; aggregate frequency/latency per query is not available (no `calls`/`mean_time`); the retained text after completion is only the *last* query per backend, not a history. It is inherently a **sampled, current-state** view, not a complete record.
- **Truncation:** query text is truncated at `track_activity_query_size` (default 1024 bytes), so long statements are cut off. [6]
- **Setup cost is the lowest of the three:** no restart, no extension. It requires `track_activities` = on (the default). [6]
- **Permissions / masking:** ordinary users see full info only for their **own** sessions; for **other** sessions many columns (including `query`) are null. "Superusers and roles with privileges of built-in role `pg_read_all_stats` can see all the information about all sessions." [6] So a monitoring role needs `pg_read_all_stats` (a GRANT, not a restart) to see other users' query text. The text shown is **raw SQL with literals** (same masking concern as logs), but you only ever capture a thin, current-moment slice.

---

## Comparison table

| Dimension | `pg_stat_statements` | Log-based (`log_min_duration_statement` / `auto_explain`) | `pg_stat_activity` sampling |
|---|---|---|---|
| **Coverage** | All executed statements, aggregated per normalized `queryid` (counts, exec/plan time, rows, WAL) [1] | All statements over the duration threshold (unless sampled); `auto_explain` adds plans [2][3] | Only statements `active` at each sample tick; misses short/completed queries [6] |
| **Granularity** | Aggregate per query shape (no per-execution detail; loses literals) [1] | Per individual execution, full raw text (+ plan) [2][3] | Per current execution snapshot; last-query-per-backend only [6] |
| **Overhead** | Shared mem ∝ `.max`; low CPU normally; `track_planning` warned as "noticeable penalty" [1] | Low for duration logging; `auto_explain.log_analyze`/`log_timing` "extremely negative impact" [3]; I/O from log volume | Low; just periodic reads of an in-memory view [6] |
| **Real-time-ness** | Near real-time cumulative counters (updated as queries run) | Delayed — only after statement completes, plus log parse/ship latency [2] | Real-time current-query snapshot ("always up-to-date") [6] |
| **Masking / security** | Good — literals normalized to `$1`; text of others' queries gated behind `pg_read_all_stats`/superuser [1] | Poor — raw SQL + literal/bind values in log files; needs external scrubbing [2] | Poor for captured text (raw SQL), but only a thin current slice; others' text gated behind `pg_read_all_stats`/superuser [6] |
| **Setup cost** | `shared_preload_libraries` → **server restart** + `CREATE EXTENSION` (elevated) [1] | Runtime `SET`/reload (no restart); `auto_explain` needs preload or session `LOAD` (superuser) [2][3] | **No restart, no extension**; needs `track_activities`=on (default) [6] |
| **Permissions** | Config-file edit + create-extension privilege; `pg_read_all_stats`/superuser to read other users' text [1] | Superuser/`SET` privilege to change GUCs; filesystem access to read logs [2] | `pg_read_all_stats`/superuser (GRANT only) to see other users' sessions [6] |
| **Availability under restricted / no-restart constraints** | **Often blocked** (restart + config + extension privilege) — only usable if already loaded by the DBA | Partially available (duration logging is runtime-togglable if you have the privilege; `auto_explain` and log access are harder) | **Always available** with a `pg_read_all_stats` GRANT; the safest fallback |

---

## MVP recommendation

1. **Baseline (always ship): `pg_stat_activity` sampling.** It is the only source guaranteed to work under a no-restart / restricted-permission private deployment — it needs at most a `GRANT pg_read_all_stats` and the default `track_activities`. Sample active sessions on an interval to surface long-running queries in real time. Accept its limits (misses short/completed queries; no aggregate latency). [6]

2. **Opportunistic enhancement: `pg_stat_statements` when already present.** Detect it at connect time (`SELECT ... FROM pg_extension WHERE extname='pg_stat_statements'` / probe the view). If loaded, use it for the *rich* enhanced-monitoring metrics (top queries by total/mean exec time, calls, rows, WAL) — with the strong bonus that it is **masking-safe by default**. Do **not** make the MVP depend on being able to enable it ourselves, since that needs a restart + extension privilege. Branch on version for the PG13 `total_exec_time`/`total_plan_time`/`wal_*` and PG14 `toplevel`/`queryid`-join changes. [1][4][5]

3. **Defer / opt-in only: log-based + `auto_explain`.** Highest fidelity (every slow query, with plans) but the **raw-SQL-with-literals masking problem** plus log-parsing latency and filesystem/superuser access make it a poor fit for a low-friction MVP in a security-sensitive private environment. Introduce later as an explicit opt-in feature with a scrubbing story. [2][3]

**One-line rationale:** `pg_stat_activity` = zero-setup, always-available floor; `pg_stat_statements` = rich + safe when the DBA already enabled it; logs/`auto_explain` = powerful but masking- and latency-costly, so post-MVP.

### Facts we could NOT fully confirm from primary sources in this pass
- The **exact quantitative** overhead of `pg_stat_statements` (e.g., "~X% throughput") — the docs describe overhead qualitatively (shared memory ∝ `.max`; `track_planning` "noticeable penalty") but give no committed percentage. [1]
- The precise PG version in which the **`IN`-list squashing display** (`$1 /*, ... */`) was introduced — it appears in current (PG18) docs as normalization behavior but the version-of-introduction was not verified here. [1]

---

## Sources

1. PostgreSQL Documentation — `pg_stat_statements` (current): https://www.postgresql.org/docs/current/pgstatstatements.html
2. PostgreSQL Documentation — Error Reporting and Logging (`log_min_duration_statement`, sampling): https://www.postgresql.org/docs/current/runtime-config-logging.html
3. PostgreSQL Documentation — `auto_explain` (current): https://www.postgresql.org/docs/current/auto-explain.html
4. PostgreSQL 13 Release Notes (planning-time tracking, `track_planning`, WAL columns in pg_stat_statements): https://www.postgresql.org/docs/release/13.0/
5. PostgreSQL 14 Release Notes (`toplevel` column, `compute_query_id` moved to core, `queryid` in pg_stat_activity/log): https://www.postgresql.org/docs/release/14.0/
6. PostgreSQL Documentation — Monitoring / Cumulative Statistics (`pg_stat_activity`, permissions, `track_activities`, `track_activity_query_size`): https://www.postgresql.org/docs/current/monitoring-stats.html
