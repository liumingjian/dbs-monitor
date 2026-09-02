# The time series table gains one named dimension (`database_name`), not a general label model

A monitored instance is one connection that may contain many databases, so the per-database metrics from `pg_stat_database` — size, cache hit ratio, deadlocks, commits and rollbacks — need somewhere to live. `metric_series` is keyed by `(instance_id, metric_id)` and has no dimension column at all; until now the database dimension was smuggled in by giving every database its own instance row. We add a single column, `database_name text NOT NULL DEFAULT ''`, making the key `(instance_id, metric_id, database_name)`, where the empty string means instance-level. `metric_sample` is unchanged.

## Considered Options

**A general label model (`labels jsonb`), Prometheus-style.** Rejected: it turns this project into a time-series database. The index and query shape of the partitioned hot path would be rewritten to serve a requirement that does not exist — beyond "database", no second dimension is expected within the next two years. Table-level monitoring, the obvious candidate, is more likely to be its own table than a time series.

**No per-database breakdown at all: sum across databases at collection time.** Rejected: it throws away the answer to "which database filled the disk", which is precisely the question that arises when one connection holds dozens of databases.

## Consequences

Because a single instance-level number now hides several databases, the aggregation rule matters and is recorded per metric in `metric_catalog`: ratios are weighted (cache hit ratio by `blks_hit + blks_read`), counts and sizes are summed. An arithmetic mean would let twenty empty databases at 100% dilute a 200 GB primary that has collapsed to 60%.

Adding a second dimension later means a second column and a second uniqueness constraint. That is the cost we accepted in exchange for not building a label engine.
