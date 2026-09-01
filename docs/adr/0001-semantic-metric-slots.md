# Metrics stay engine-namespaced; a small set of semantic slots points at them

MySQL support is coming, and the metric catalogue is entirely `pg.*`. Rather than renaming all 41 metrics into a neutral `db.*` namespace, we define nine **semantic slots** — throughput, connections, connection saturation, probe latency, rollback rate, replication lag, cache hit ratio, storage usage, deadlocks — where each slot is an indirection that resolves per engine to a concrete metric ID. Existing metric IDs are untouched. The cluster overview, the instance list, and alert rule templates may address metrics only by slot; the instance workbench may address concrete IDs.

## Considered Options

**Rename everything to a neutral namespace.** Rejected: it forces a migration of historical samples and of every stored alert rule, and several metrics have no neutral meaning at all — `pg.replication_slot.retained_wal_bytes` and `pg.prepared_xacts.count` have no MySQL counterpart, so a neutral name for them would be a lie.

**Per-engine namespaces with no abstraction, mapped in the UI.** Rejected: alert rule templates would have to be written once per engine, and one rule could never span a mixed fleet. In a 500-instance fleet running two engines that cost lands on exactly the handful of metrics used most often.

The slots put the abstraction only where it is actually needed. Cross-engine reuse is genuinely required for the six or seven numbers on the overview and in the alert templates — not for all 41 metrics.

## Consequences

Engine-private metrics are deliberately unreachable from the overview, the instance list, and alert rule templates. That is the point: a metric with no slot is a metric that cannot silently acquire a misleading cross-engine meaning. When a metric turns out to exist on both engines, adding a slot is cheap and backwards compatible.
