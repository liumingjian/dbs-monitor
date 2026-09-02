# Metrics stay engine-namespaced; a small set of semantic slots points at them

MySQL support is coming, and the metric catalogue is entirely `pg.*`. Rather than renaming all 41 metrics into a neutral `db.*` namespace, we define nine **semantic slots** — throughput, connections, connection saturation, probe latency, rollback rate, replication lag, cache hit ratio, storage usage, deadlocks — where each slot is an indirection that resolves per engine to a concrete metric ID. Existing metric IDs are untouched. The cluster overview, the instance list, and alert rule templates may address metrics only by slot; the instance workbench may address concrete IDs.

## Considered Options

**Rename everything to a neutral namespace.** Rejected: it forces a migration of historical samples and of every stored alert rule, and several metrics have no neutral meaning at all — `pg.replication_slot.retained_wal_bytes` and `pg.prepared_xacts.count` have no MySQL counterpart, so a neutral name for them would be a lie.

**Per-engine namespaces with no abstraction, mapped in the UI.** Rejected: alert rule templates would have to be written once per engine, and one rule could never span a mixed fleet. In a 500-instance fleet running two engines that cost lands on exactly the handful of metrics used most often.

The slots put the abstraction only where it is actually needed. Cross-engine reuse is genuinely required for the six or seven numbers on the overview and in the alert templates — not for all 41 metrics.

## One slot, one unit — and the storage slot

A slot is only useful if everything that consumes it can consume it blindly. That forces one rule the
first draft of the catalogue broke: **every binding of a slot must be in the same unit.** 容量水位 was
originally bound twice — `host.disk.usage_percent` (a percentage, engine-agnostic) and
`pg.database.size_bytes` (a byte count, PostgreSQL). A slot whose value is sometimes a percentage and
sometimes a size cannot be read by a generic consumer: the overview's usage list and an alert template's
threshold would silently switch dimension per engine, and the screen would still look right.

So the storage slot means **the disk watermark of the host**, and the database-size binding is gone. The
spec's table lists 容量水位 as 「库级 + 主机」 with both metrics; what survives of the database-level half is
the metric itself, which stays in the catalogue and stays reachable — the instance workbench may address
concrete metric ids, and 「哪个库在吃磁盘」 is a workbench question, not a fleet-level one. Nothing on the
overview, the instance list or an alert template could have used a byte count under a slot named
「容量水位」 anyway. The slot count is still nine.

The other half of the rule: a slot bound to an **engine-agnostic** metric resolves on *every* engine.
`host.disk.usage_percent` measures the host, not the product, so asking the storage slot on any engine
answers with it. Without that, callers would have to know which slots are host-side and pass
`AGNOSTIC` themselves — a constant in the caller that makes the indirection inert.

## Consequences

Engine-private metrics are deliberately unreachable from the overview, the instance list, and alert rule templates. That is the point: a metric with no slot is a metric that cannot silently acquire a misleading cross-engine meaning. When a metric turns out to exist on both engines, adding a slot is cheap and backwards compatible.
